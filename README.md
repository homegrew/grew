# 🥤 grew

> *A lean, mean, package-managing machine. In Go.*

[![Go Version][go-badge]][go-url]

`gobrew` is what happens when you look at your package manager and think: *"This could be so much simpler."* Deterministic installs. Clean symlinks. A doctor that actually tells you what's wrong. No drama.

> 💬 **A word from the author:**
> *I've been a die-hard Homebrew user for longer than I care to admit. brew and I? We go way back. Late nights, broken PATH, the works — and I loved every minute of it. I love brew so much, in fact, that I thought: "What if I just… made it better?" Audacious? Absolutely. Foolish? Possibly. Fun? You bet. grew is my love letter to brew — written in Go, with a cheeky grin.*

---

## ✨ What it does

- 📦 **Formula + cask installs** with SHA256 verification (no funny business)
- 🔗 **Deterministic linking** with opt symlinks and dry-run support (look before you link)
- 🌳 **Dependency resolver** with an optional tree view (for the visually inclined)
- 🩺 **Doctor** that checks perms, HTTPS, broken links, and stale kegs (your package manager has trust issues, and rightfully so)
- 🐚 **Alias + shellenv helpers** so your workflows stay snappy

---

## 🚀 Getting Started

**Prerequisites:** Go 1.26+, `git`, `curl`, and a dream.

```bash
git clone https://github.com/homegrew/grew.git
cd grew
go build -o gobrew .
./gobrew help
```

That's it. No dark rituals. No 47-step setup guide.

---

## 📖 Usage

```bash
./gobrew install jq              # the classic
./gobrew install --cask firefox  # going big
./gobrew link jq                 # stitch it in
./gobrew deps --tree jq          # what hath jq wrought
./gobrew upgrade                 # stay fresh
./gobrew cleanup -n              # peek before you sweep
```

---

## 🗺️ Commands

| Command | What it does |
|---|---|
| `install` | Install a formula or cask |
| `uninstall` | Send it to the void |
| `list` | See what you've collected |
| `info` | Stalk a package |
| `search` | Find the thing |
| `link` | Weave a formula into your PATH |
| `unlink` | Cut the thread |
| `update` | Refresh tap definitions |
| `upgrade` | Get the new hotness |
| `outdated` | The hall of shame |
| `cleanup` | Marie Kondo your Cellar |
| `deps` | Dependency spelunking |
| `alias` | Name things your way |
| `doctor` | It's not a bug, it's a misconfiguration |
| `config` | What grew thinks it knows |
| `shellenv` | Wire up your shell |
| `help` | You got this |

---

## ⚙️ Configuration

grew keeps its stuff tidy under one roof. Tweak it with env vars:

| Variable | Default | What it is |
|---|---|---|
| `GOBREW_PREFIX` | `~/.gobrew` | The kingdom |
| `GOBREW_APPDIR` | `~/Applications` | Where casks live |

Everything else flows from the prefix:

```
~/.gobrew/
├── Cellar/   ← installed packages
├── Taps/     ← formula definitions
├── bin/      ← symlinked binaries
└── tmp/      ← ephemeral stuff
```

**Shell setup** (pick your flavour):

```bash
# bash / zsh
eval "$(./gobrew shellenv)"

# fish
./gobrew shellenv fish | source
```

---

## 🛠️ Development

```bash
go test ./...
```

**Project layout:**

```
grew/
├── cmd/   ← CLI commands (the face)
├── pkg/   ← cellar, formula, tap, linker, downloader, deps (the guts)
└── taps/  ← embedded formulas and casks (the knowledge)
```

---

## 🗺️ Roadmap

Got ideas? Bugs? Grievances? → [Open an issue](https://github.com/homegrew/grew/issues)

Hot takes on the list:
- Better tap sync
- Richer doctor checks
- Windows support (one day, probably, maybe)

---

## 🤝 Contributing

1. Fork it
2. Branch it (`git checkout -b feature/your-cool-thing`)
3. Commit it (`git commit -m "Add the cool thing"`)
4. Push it (`git push origin feature/your-cool-thing`)
5. PR it

PRs welcome. Drama not so much.

---

## 📄 License

No license file yet — add a `LICENSE` to clarify what others can and can't do with your code. (It's the responsible thing to do. We believe in you.)

---

## 📬 Contact

- 🐛 [Open an issue](https://github.com/homegrew/grew/issues)
- 🔗 [Project on GitHub](https://github.com/homegrew/grew)

---

## 💛 Acknowledgments

- [Best-README-Template](https://github.com/othneildrew/Best-README-Template) — the scaffold beneath the scaffold
- Everyone who ever squinted at a wall of package manager output and thought *"there has to be a better way"*

[go-badge]: https://img.shields.io/badge/go-1.26-00ADD8?style=for-the-badge&logo=go&logoColor=white
[go-url]: https://go.dev
