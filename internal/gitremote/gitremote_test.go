package gitremote

import "testing"

func TestNormalize(t *testing.T) {
	for url, want := range map[string]string{
		"https://github.com/vtimame/istok.sh.git":      "github.com/vtimame/istok.sh",
		"https://user@GitHub.com/vtimame/istok.sh/":    "github.com/vtimame/istok.sh",
		"git@github.com:vtimame/istok.sh.git":          "github.com/vtimame/istok.sh",
		"ssh://git@github.com:22/vtimame/istok.sh.git": "github.com/vtimame/istok.sh",
		"ssh://git@gitlab.local:2222/group/app":        "gitlab.local:2222/group/app",
		"file:///srv/git/app.git":                      "/srv/git/app",
		"/srv/git/app.git":                             "/srv/git/app",
		"../app":                                       "../app",
		"":                                             "",
	} {
		if got := Normalize(url); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestMatches(t *testing.T) {
	repository := Identity{Remotes: []string{"github.com/a/b", "gitlab.com/a/b"}}

	if !repository.Matches(Identity{Remotes: []string{"gitlab.com/a/b"}}) {
		t.Error("a common remote should match")
	}
	if repository.Matches(Identity{Remotes: []string{"github.com/a/b"}, Subdir: "apps/web"}) {
		t.Error("another folder of the repository should not match")
	}
	if repository.Matches(Identity{}) {
		t.Error("an identity without remotes should not match")
	}
}
