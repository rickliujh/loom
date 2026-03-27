---
layout: home

hero:
  name: Loom
  text: Automate the last mile of your GitOps
  tagline: Declarative, composable modules that turn parameters into pull requests. One command, done.
  actions:
    - theme: brand
      text: Get Started
      link: /guide/getting-started
    - theme: alt
      text: What is Loom?
      link: /guide/what-is-loom
    - theme: alt
      text: GitHub
      link: https://github.com/rickliujh/loom

features:
  - title: Declarative Workflows
    details: Describe what should happen in a loom.yaml. Loom handles cloning, rendering, committing, pushing, and opening PRs.
  - title: Template Everything
    details: File contents, file paths, shell commands, commit messages, PR titles — every string is a Go template.
  - title: Composable Modules
    details: Build small, focused modules and compose them. A parent module orchestrates child modules with parameters flowing down.
  - title: Library First, CLI Fallback
    details: Embedded Go libraries for Git, GitHub/GitLab APIs, and YAML patching. Falls back to local CLI tools when needed.
  - title: Patch, Don't Rewrite
    details: Strategic Merge Patch and JSON6902 let you surgically modify existing YAML files without replacing them entirely.
  - title: Preview Before You Push
    details: Dry-run mode shows what would happen. Diff mode shows the actual file changes. Local-run mode lets you inspect the result.
---
