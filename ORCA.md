# ORCA.md — unattended work

You are here because your prompt started with `[orca:`. Nobody will answer
you: what you cannot decide with the task and `AGENTS.md` in front of you is
not guessed, it is said in the pull request. Everything in `AGENTS.md` still
applies, starting with: **you never merge anything and never touch `main`**.

## The task

It comes in an issue of this repository. Its «Terminado cuando» / «Done when»
section is the exit condition; whatever of the scope you cannot do goes under
«No hecho» / «Not done», it is not filled in with what seems reasonable.

## Before opening the pull request

Not optional: fetch `origin/main` and merge it into your branch
(`git fetch origin && git merge origin/main`), and run locally exactly what CI
runs — the command in `AGENTS.md`, «What CI checks». A skipped step counts as
red. Paste the output in the pull request.

## The pull request

The first line of its body closes the issue:

```
Closes #<number>
```

**Every pull request ends with these three lines**, whether it comes from an
issue or from something you found along the way:

```markdown
- **What changes and why:**
- **Tried and discarded:**
- **Left unfinished:**
```

The diff already says *what* changed. What no commit says is what was tried,
did not work and why — and that is exactly what the next session will try
again if it cannot read it. If a line does not apply, write «nothing»: a blank
line does not distinguish «there was none» from «I did not think about it».

If the issue asks for its own closing block, add it as well; it does not
replace these three lines.

## Coming back to an existing pull request

Your previous work is on the branch: do not start over, do not open another
pull request and do not change branch. Do what the latest review comment asks,
not one line more, and comment what you changed with the output pasted.
