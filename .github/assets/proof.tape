# Launch demo: an agent edits the code after its tests passed and says
# "done"; Istok refuses the stale check and the rerun catches the bug.
#
# Record it from the repository root after `make build`:
#
#   vhs .github/assets/proof.tape
#
# The scene is prepared by proof-setup.sh in a temporary directory.
Output .github/assets/proof.gif

Set Shell bash
Set FontSize 14
Set LineHeight 1.15
Set Width 1100
Set Height 440
Set Padding 18
Set Theme "Catppuccin Mocha"
Set TypingSpeed 45ms

Hide
Type `. .github/assets/proof-setup.sh && PS1='$ ' && clear`
Enter
Sleep 8s
Type "clear"
Enter
Show

Sleep 500ms
Type "# The agent ran the tests through Istok:"
Enter
Type "istok task show 1 | grep -A1 Checks"
Enter
Sleep 3s

Type "# Then it 'simplified the rounding' and said: all tests pass, done."
Enter
Type "sed -i 's/int64(i) < leftover/int64(i) <= leftover/' split.go"
Enter
Sleep 1s
Type "istok task show 1 | grep -A1 Checks"
Enter
Sleep 3500ms

Type "# Closing the task on that check:"
Enter
Type `istok task done 1 --run-id=$RUN --validation-id=$CHECK --expected-revision=$REV --note="done"`
Enter
Sleep 4s

Type "# So the tests run again, by Istok:"
Enter
Type `istok run validate --lease=$LEASE $RUN -- go test ./... > /dev/null`
Enter
Sleep 3s
Type "istok task show 1 | grep -A2 Checks"
Enter
Sleep 6s
