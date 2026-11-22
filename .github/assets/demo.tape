# README demo: `istok task show` for a task a real Claude Code agent finished.
#
# Record it from the repository root:
#
#   ISTOK_DEMO_DIR=~/work/istok/splitbill vhs .github/assets/demo.tape
#
# ISTOK_DEMO_DIR is an Istok project whose task #2 the agent completed.
Output .github/assets/demo.gif

Set Shell bash
Set FontSize 13
Set LineHeight 1.15
Set Width 900
Set Height 760
Set Padding 18
Set Theme "Catppuccin Mocha"
Set TypingSpeed 60ms

Hide
Type `PS1='$ ' && cd "$ISTOK_DEMO_DIR" && clear`
Enter
Show

Sleep 600ms
Type "istok task show 2"
Sleep 400ms
Enter
Sleep 9s
