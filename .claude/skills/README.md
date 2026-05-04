# Skills — keyforge-hid

Project-scoped skills go here. Each skill is a folder with at least `SKILL.md`:

```
skills/
└── <skill-name>/
    └── SKILL.md
```

`SKILL.md` frontmatter:

```markdown
---
name: skill-name
description: When this skill applies (used by Claude to auto-invoke)
allowed-tools: Read, Edit, Bash(go test *)
---

Step-by-step procedure.
```

Empty for now — convert recurring procedures (e.g. "add a new device support",
"reverse-engineer an HID report") into skills as they emerge.
