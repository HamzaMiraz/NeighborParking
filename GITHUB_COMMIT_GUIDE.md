# NeighborParking GitHub and Commit Guide

This guide uses Windows PowerShell commands and assumes the project is located at:

```powershell
Set-Location 'D:\GoLang\NeighborParking'
```

Git is already installed on this computer. GitHub CLI (`gh`) is optional and is not currently installed.

## 1. Important rules before uploading

- Never commit `.env`, passwords, access tokens, production database credentials, or private keys.
- Keep shareable placeholders in `.env.example` and store real values only in `.env` or the deployment platform's secret manager.
- Review staged changes before every commit with `git diff --cached`.
- Run the tests before pushing important changes.
- Do not use `git push --force` on `main`. If rewriting your own feature branch is unavoidable, use `git push --force-with-lease`.
- Do not commit `.tools`, `bin`, compiled `.exe` files, logs, IDE settings, or local database data. The existing `.gitignore` excludes these.

Confirm that private/generated files and the internal prompt document are ignored:

```powershell
git check-ignore -v .env .tools bin NEIGHBORPARKING_MASTER_PROMPT.md
```

## 2. One-time Git identity setup

Replace the example values with the name and email you want displayed on GitHub commits. Using an email already added to your GitHub account lets GitHub associate the commits with your profile.

```powershell
git config --global user.name "Your Name"
git config --global user.email "your-email@example.com"
git config --global init.defaultBranch main
git config --global core.autocrlf true
```

Check the result:

```powershell
git config --global --list
```

## 3. Create the GitHub repository

In GitHub:

1. Sign in and choose **New repository**.
2. Use a name such as `NeighborParking`.
3. Add a short description, for example: `A real-time community parking manager built with Go, PostgreSQL, WebSockets, and Docker.`
4. Choose **Public** if this is for your CV, or **Private** while it is unfinished.
5. Do not initialize the GitHub repository with a README, `.gitignore`, or license because those files already exist locally.
6. Create the repository and copy its HTTPS URL.

The URL will look like:

```text
https://github.com/YOUR_USERNAME/NeighborParking.git
```

## 4. First commit and first push

This folder is not currently initialized as a Git repository. Run these commands once, replacing the remote URL:

```powershell
Set-Location 'D:\GoLang\NeighborParking'
git init
git branch -M main
git status
git add --all
git status
git diff --cached --stat
git diff --cached
git commit -m "feat: build initial NeighborParking application"
git remote add origin https://github.com/YOUR_USERNAME/NeighborParking.git
git remote -v
git push -u origin main
```

When GitHub authentication opens, sign in through the browser or Windows Git Credential Manager. A normal GitHub account password cannot be used as a Git password. If prompted for a token instead, create a GitHub personal access token with repository access and use that token as the password.

Verify the upload by refreshing the repository page on GitHub.

### If `origin` already exists

Inspect it first:

```powershell
git remote -v
```

If the URL is wrong, replace it:

```powershell
git remote set-url origin https://github.com/YOUR_USERNAME/NeighborParking.git
```

## 5. Recommended check before every important commit

Use this sequence from the project root:

```powershell
Set-Location 'D:\GoLang\NeighborParking'
git status
git diff
.\scripts\test.ps1
node --check .\internal\httpapi\web\app.js
node --check .\scripts\browser-verify.mjs
```

If the Docker application is running and the demo database is seeded, the deeper checks are:

```powershell
& 'C:\Program Files\Go\bin\go.exe' run .\cmd\verify
node .\scripts\browser-verify.mjs
docker compose ps
```

The full verifier intentionally creates a new QA community. Older QA communities can be archived through the application/database if repeated verifier runs make the demo dashboard crowded.

## 6. Everyday update, commit, and push workflow

Use this workflow whenever you update the project:

```powershell
Set-Location 'D:\GoLang\NeighborParking'
git switch main
git pull --rebase origin main
git status
git diff
.\scripts\test.ps1
git add --all
git diff --cached --stat
git diff --cached
git commit -m "feat: describe the feature you added"
git push origin main
```

If there are no changed files, Git will report `nothing to commit, working tree clean`. Do not create an empty commit just to show activity.

### Stage only selected files

Staging selected files is safer when unrelated work is present:

```powershell
git add README.md
git add internal\httpapi\handlers.go
git diff --cached
git commit -m "docs: update setup and API instructions"
```

To select individual sections inside files:

```powershell
git add --patch
```

## 7. Commit message tips

Write a short command-style summary describing why the change matters. A useful convention is:

```text
type: short description
```

Common types:

| Type | Use it for | Example |
|---|---|---|
| `feat` | New user-facing capability | `feat: add parking occupancy history` |
| `fix` | Bug fix | `fix: prevent duplicate slot check-in` |
| `docs` | Documentation only | `docs: add deployment instructions` |
| `test` | Tests or verification tools | `test: cover membership access revocation` |
| `refactor` | Internal restructuring without behavior changes | `refactor: separate membership queries` |
| `style` | Formatting or visual styling | `style: improve mobile parking grid` |
| `chore` | Maintenance and tooling | `chore: update Docker configuration` |
| `ci` | GitHub Actions changes | `ci: run Go race tests on pull requests` |

For a larger commit, add a body:

```powershell
git commit -m "feat: add admin force checkout" -m "Adds authorization checks, audit logging, database updates, and real-time WebSocket events."
```

Keep commits focused. Prefer two clear commits over one commit mixing an unrelated bug fix, redesign, and documentation update.

## 8. Feature branch workflow

For significant work, use a branch instead of changing `main` directly:

```powershell
git switch main
git pull --rebase origin main
git switch -c feature/parking-history
```

Work, test, and commit normally:

```powershell
git status
git add --all
git diff --cached
git commit -m "feat: add parking history page"
git push -u origin feature/parking-history
```

Open a pull request on GitHub from `feature/parking-history` into `main`. The workflow in `.github/workflows/ci.yml` will run automated checks on GitHub.

After the pull request is merged:

```powershell
git switch main
git pull --rebase origin main
git branch -d feature/parking-history
git push origin --delete feature/parking-history
```

Only delete the remote branch after it is merged and no longer needed.

## 9. See history and current differences

```powershell
git status
git log --oneline --graph --decorate --all -20
git diff
git diff --cached
git show --stat HEAD
git remote -v
git branch -vv
```

Useful file-specific commands:

```powershell
git log --oneline -- README.md
git diff HEAD -- README.md
```

## 10. Safely correct mistakes

### Unstage a file but keep its changes

```powershell
git restore --staged path\to\file.go
```

### Discard uncommitted changes in one file

This permanently removes that file's local changes, so inspect `git diff` first:

```powershell
git diff -- path\to\file.go
git restore path\to\file.go
```

### Change the most recent commit message before pushing

```powershell
git commit --amend -m "fix: corrected commit description"
```

### Add a forgotten file to the most recent unpushed commit

```powershell
git add path\to\forgotten-file.go
git commit --amend --no-edit
```

### Undo a commit that was already pushed

Use `revert`; it creates a safe inverse commit and preserves shared history:

```powershell
git log --oneline -10
git revert COMMIT_HASH
git push origin main
```

### Save unfinished work temporarily

```powershell
git stash push -u -m "unfinished parking map work"
git pull --rebase origin main
git stash pop
```

Inspect saved work with `git stash list` before deleting any stash.

## 11. Resolve a normal merge conflict

If `git pull --rebase` reports a conflict:

```powershell
git status
```

Open each conflicted file, find the `<<<<<<<`, `=======`, and `>>>>>>>` markers, keep the correct combined content, and remove the markers. Then run:

```powershell
git add path\to\resolved-file.go
git rebase --continue
```

Repeat until complete, run tests, and push. To abandon the rebase and return to the earlier state:

```powershell
git rebase --abort
```

Do not use `--allow-unrelated-histories` unless you intentionally created two independent repositories and understand how their files should be combined.

## 12. If a secret is accidentally staged or committed

If it is staged but not committed:

```powershell
git restore --staged .env
```

Make sure `.env` remains listed in `.gitignore`.

If a real password or token was committed or pushed, removing the file in a later commit is not enough because the value remains in history. Immediately revoke/rotate the credential, stop using it, and then clean the repository history with an appropriate tool such as `git filter-repo`. Treat the old credential as compromised.

Check whether a sensitive file is already tracked:

```powershell
git ls-files .env
```

If `.env` appears and has not yet been pushed, remove it only from Git tracking while retaining the local file:

```powershell
git rm --cached .env
git commit -m "chore: stop tracking local environment file"
```

## 13. Version tags and GitHub releases

After a stable milestone on `main`:

```powershell
git switch main
git pull --rebase origin main
.\scripts\test.ps1
git tag -a v0.1.0 -m "NeighborParking MVP v0.1.0"
git push origin v0.1.0
```

Create a GitHub Release from that tag and summarize the important features, setup steps, screenshots, and known MVP boundaries. Increase versions intentionally:

- `v0.1.1` for compatible bug fixes
- `v0.2.0` for new compatible features before version 1.0
- `v1.0.0` when the public interface and deployment are considered stable

## 14. Optional GitHub CLI workflow

GitHub CLI is not required. To install it later with Windows Package Manager:

```powershell
winget install --id GitHub.cli
gh auth login
```

After `git init` and the first local commit, GitHub CLI can create and push the remote repository:

```powershell
gh repo create NeighborParking --public --source . --remote origin --push
```

Use `--private` instead of `--public` when appropriate.

## 15. Quick command card

For normal updates, this is the short version:

```powershell
Set-Location 'D:\GoLang\NeighborParking'
git pull --rebase origin main
git status
git diff
.\scripts\test.ps1
git add --all
git diff --cached
git commit -m "type: clear description"
git push origin main
```

Before closing your work session, `git status` should normally show:

```text
On branch main
Your branch is up to date with 'origin/main'.
nothing to commit, working tree clean
```
