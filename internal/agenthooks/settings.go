package agenthooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// The settings files belong to the user and to other tools. Istok edits only
// its own hook entries: other keys keep their order and value, so the file
// reads as before apart from the hooks Istok adds or removes. encoding/json
// maps would sort the keys, so objects are handled as ordered member lists;
// this avoids a JSON-patching dependency for two levels of nesting.

// member is one key of a JSON object, with its raw value.
type member struct {
	Key   string
	Value json.RawMessage
}

type object []member

func parseObject(data []byte) (object, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return object{}, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("expected a JSON object")
	}

	result := object{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("expected an object key")
		}

		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		result = append(result, member{Key: key, Value: value})
	}

	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err == nil {
		return nil, errors.New("unexpected data after the JSON object")
	}

	return result, nil
}

func (o object) get(key string) (json.RawMessage, bool) {
	for _, value := range o {
		if value.Key == key {
			return value.Value, true
		}
	}

	return nil, false
}

// set replaces the key in place or appends it.
func (o object) set(key string, value json.RawMessage) object {
	for i := range o {
		if o[i].Key == key {
			o[i].Value = value
			return o
		}
	}

	return append(o, member{Key: key, Value: value})
}

func (o object) remove(key string) object {
	result := o[:0]
	for _, value := range o {
		if value.Key != key {
			result = append(result, value)
		}
	}

	return result
}

func (o object) marshal() (json.RawMessage, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for i, value := range o {
		if i > 0 {
			buffer.WriteByte(',')
		}

		key, err := marshal(value.Key)
		if err != nil {
			return nil, err
		}
		buffer.Write(key)
		buffer.WriteByte(':')
		buffer.Write(value.Value)
	}
	buffer.WriteByte('}')

	return buffer.Bytes(), nil
}

// handler is the part of a hook handler Istok reads.
type handler struct {
	Command string `json:"command"`
}

// istokHandler is a handler Istok writes.
type istokHandler struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// Change is the result of an install or uninstall.
type Change struct {
	Path    string  `json:"path"`
	Changed bool    `json:"changed"`
	Events  []Event `json:"events"`
	Backup  string  `json:"backup,omitempty"`
}

// Install writes the hooks into the settings file, replacing earlier Istok
// hooks of the agent and keeping everything else.
func Install(path string, agent Agent, hooks []Hook, executable string) (Change, error) {
	return rewrite(path, agent, installChange(agent, hooks, executable))
}

func installChange(agent Agent, hooks []Hook, executable string) func(object) (object, error) {
	return func(events object) (object, error) {
		for _, hook := range hooks {
			group := object{}
			if hook.Matcher != "" {
				matcher, _ := marshal(hook.Matcher)
				group = group.set("matcher", matcher)
			}

			handlers, err := marshal([]istokHandler{{Type: "command", Command: Command(executable, agent, hook.Event), Timeout: hookTimeoutSeconds}})
			if err != nil {
				return nil, err
			}
			group = group.set("hooks", handlers)

			encoded, err := group.marshal()
			if err != nil {
				return nil, err
			}

			var groups []json.RawMessage
			if existing, ok := events.get(hook.Name); ok {
				if err := json.Unmarshal(existing, &groups); err != nil {
					return nil, fmt.Errorf("read hooks.%s: %w", hook.Name, err)
				}
			}
			groups = append(groups, encoded)

			value, err := marshal(groups)
			if err != nil {
				return nil, err
			}
			events = events.set(hook.Name, value)
		}

		return events, nil
	}
}

// Uninstall removes the agent's Istok hooks from the settings file.
func Uninstall(path string, agent Agent) (Change, error) {
	return rewrite(path, agent, func(events object) (object, error) { return events, nil })
}

// Installed lists the Istok hook events in the settings file.
func Installed(path string, agent Agent) ([]Event, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, err
	}

	events, err := installedIn(data, agent)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	return events, nil
}

// Snippet is the settings content Istok would write into an empty file.
func Snippet(agent Agent, hooks []Hook, executable string) ([]byte, error) {
	formatted, _, err := transform(nil, agent, installChange(agent, hooks, executable))
	return formatted, err
}

// rewrite removes the agent's Istok hooks, applies change and writes the file
// back when it differs.
func rewrite(path string, agent Agent, change func(object) (object, error)) (Change, error) {
	result := Change{Path: path, Events: []Event{}}

	original, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return result, err
	}

	formatted, empty, err := transform(original, agent, change)
	if err != nil {
		return result, fmt.Errorf("update %s: %w", path, err)
	}

	installed, err := installedIn(formatted, agent)
	if err != nil {
		return result, err
	}
	result.Events = installed

	if same(original, formatted) || (len(original) == 0 && empty) {
		return result, nil
	}

	if len(original) > 0 {
		result.Backup = path + ".istok.bak"
		if err := writeFile(result.Backup, original); err != nil {
			return result, fmt.Errorf("back up %s: %w", path, err)
		}
	}
	if err := writeFile(path, formatted); err != nil {
		return result, err
	}

	result.Changed = true

	return result, nil
}

// transform returns the formatted settings with the agent's Istok hooks
// replaced by change, and whether no hooks are left.
func transform(original []byte, agent Agent, change func(object) (object, error)) ([]byte, bool, error) {
	root, err := parseObject(original)
	if err != nil {
		return nil, false, err
	}

	events, err := hookEvents(root)
	if err != nil {
		return nil, false, err
	}

	events, err = withoutIstok(events, agent)
	if err != nil {
		return nil, false, err
	}

	events, err = change(events)
	if err != nil {
		return nil, false, err
	}

	if len(events) == 0 {
		root = root.remove("hooks")
	} else {
		encoded, err := events.marshal()
		if err != nil {
			return nil, false, err
		}
		root = root.set("hooks", encoded)
	}

	encoded, err := root.marshal()
	if err != nil {
		return nil, false, err
	}

	var compact, formatted bytes.Buffer
	if err := json.Compact(&compact, encoded); err != nil {
		return nil, false, err
	}
	if err := json.Indent(&formatted, compact.Bytes(), "", "  "); err != nil {
		return nil, false, err
	}
	formatted.WriteByte('\n')

	return formatted.Bytes(), len(events) == 0, nil
}

func hookEvents(root object) (object, error) {
	value, ok := root.get("hooks")
	if !ok {
		return object{}, nil
	}

	events, err := parseObject(value)
	if err != nil {
		return nil, fmt.Errorf("hooks: %w", err)
	}

	return events, nil
}

// withoutIstok drops the agent's Istok handlers, then matcher groups and
// events left empty by that.
func withoutIstok(events object, agent Agent) (object, error) {
	result := object{}

	for _, event := range events {
		var groups []json.RawMessage
		if err := json.Unmarshal(event.Value, &groups); err != nil {
			// Not a list of matcher groups: not Istok's, keep it as is.
			result = append(result, event)
			continue
		}

		kept := []json.RawMessage{}
		changed := false
		for _, raw := range groups {
			group, err := parseObject(raw)
			if err != nil {
				kept = append(kept, raw)
				continue
			}

			handlers, ok := group.get("hooks")
			if !ok {
				kept = append(kept, raw)
				continue
			}

			var items []json.RawMessage
			if err := json.Unmarshal(handlers, &items); err != nil {
				kept = append(kept, raw)
				continue
			}

			remaining := []json.RawMessage{}
			for _, item := range items {
				var value handler
				if json.Unmarshal(item, &value) == nil && isIstokCommand(value.Command, agent) {
					changed = true
					continue
				}
				remaining = append(remaining, item)
			}

			if len(remaining) == len(items) {
				kept = append(kept, raw)
				continue
			}
			if len(remaining) == 0 {
				continue
			}

			encoded, err := marshal(remaining)
			if err != nil {
				return nil, err
			}
			group = group.set("hooks", encoded)
			if raw, err = group.marshal(); err != nil {
				return nil, err
			}
			kept = append(kept, raw)
		}

		if !changed {
			result = append(result, event)
			continue
		}
		if len(kept) == 0 {
			continue
		}

		encoded, err := marshal(kept)
		if err != nil {
			return nil, err
		}
		result = append(result, member{Key: event.Key, Value: encoded})
	}

	return result, nil
}

func installedIn(data []byte, agent Agent) ([]Event, error) {
	root, err := parseObject(data)
	if err != nil {
		return nil, err
	}
	events, err := hookEvents(root)
	if err != nil {
		return nil, err
	}

	result := []Event{}
	for _, value := range events {
		var groups []struct {
			Hooks []handler `json:"hooks"`
		}
		if json.Unmarshal(value.Value, &groups) != nil {
			continue
		}
		for _, group := range groups {
			for _, item := range group.Hooks {
				if event, ok := commandEvent(item.Command, agent); ok {
					result = append(result, event)
				}
			}
		}
	}

	// Report events in Istok's order, whatever the order in the file.
	slices.SortStableFunc(result, func(a, b Event) int {
		return slices.Index(allEvents, a) - slices.Index(allEvents, b)
	})

	return result, nil
}

// commandEvent returns the event of an Istok hook command.
func commandEvent(command string, agent Agent) (Event, bool) {
	if !isIstokCommand(command, agent) {
		return "", false
	}

	for _, event := range allEvents {
		if bytes.HasSuffix([]byte(command), []byte(" "+string(event))) {
			return event, true
		}
	}

	return "", false
}

// same compares settings ignoring formatting, so an untouched file is not
// rewritten just to reformat it.
func same(original, formatted []byte) bool {
	if len(bytes.TrimSpace(original)) == 0 {
		return false
	}

	var a, b bytes.Buffer
	if json.Compact(&a, original) != nil || json.Compact(&b, formatted) != nil {
		return false
	}

	return bytes.Equal(a.Bytes(), b.Bytes())
}

// writeFile replaces the file atomically and keeps the mode of an existing
// file.
func writeFile(path string, data []byte) error {
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}

	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)

	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}

	return os.Rename(name, path)
}

// marshal encodes without HTML escaping, so commands with & or > in other
// tools' hooks keep their text.
func marshal(value any) (json.RawMessage, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}

	return bytes.TrimRight(buffer.Bytes(), "\n"), nil
}
