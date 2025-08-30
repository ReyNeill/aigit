- [x] working version control system

Do not push this file to GitHub. It's ignored via .gitignore, but if it's already tracked run:
  git rm --cached TODO.md

Launch Checklist (You)
- [ ] Create GitHub repo (e.g., github.com/YOUR_GH_USER/aigit)
- [ ] Update go.mod module path to github.com/YOUR_GH_USER/aigit and run `go mod tidy`
- [ ] Push code to GitHub main branch
- [ ] Set GitHub Actions secrets (optional for release): OPENROUTER_API_KEY (if running online tests)
- [ ] Create v0.1.0 tag to trigger GoReleaser (publishes binaries)
- [ ] (Optional) Create Homebrew tap repo (homebrew-tap) and configure .goreleaser.yaml

Marketing / Demo
- [ ] Record quick demo GIFs: first-save activation, mid-merge checkpoint, remote apply
- [ ] Write a short tweet/thread announcing Aigit
- [ ] Draft a YouTube demo script (2–4 min)

Docs polish
- [ ] Replace README install line with your real go install path
- [ ] Add screenshots/GIFs to README

Product polish (future)
- [ ] Prune policy: keep last N or TTL for checkpoints
- [ ] VS Code extension: panel for checkpoints + summaries
- [ ] Local HTTP endpoint for agent-supplied summaries
