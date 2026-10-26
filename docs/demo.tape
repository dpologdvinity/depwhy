# Records docs/demo.gif. Regenerate with: go build -o /tmp/depwhy-demo/depwhy . && vhs docs/demo.tape
# (needs vhs, ttyd, ffmpeg and network access for the --online step).
Output docs/demo.gif

Set Shell "bash"
Set FontSize 18
Set Width 1100
Set Height 640
Set Padding 20
Set Theme "Dracula"
Set TypingSpeed 45ms

Hide
Type "export PATH=/tmp/depwhy-demo:$PATH PS1='$ ' && cd docs/demo && clear"
Enter
Show

Type "depwhy qs"
Sleep 400ms
Enter
Sleep 4s

Type "clear"
Enter
Type "depwhy --remove mocha | head -12"
Sleep 400ms
Enter
Sleep 4s

Type "clear"
Enter
Type "depwhy --dupes | head -9"
Sleep 400ms
Enter
Sleep 4s

Type "clear"
Enter
Type "depwhy --online left-pad"
Sleep 400ms
Enter
Sleep 6s
