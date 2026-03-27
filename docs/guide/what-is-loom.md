# What is Loom?

Loom automates the last mile of your GitOps.

You've adopted GitOps. Your applications deploy through Git. But every time you onboard a new service, add a new environment, or wire up a new team, you find yourself doing the same thing: copying YAML files, editing five fields, opening a PR, and moving on. It's not hard work. It's just tedious, error-prone, and never worth building a whole internal tool for.

Loom sits in that gap. You describe the repetitive part once as a **module** — a folder of templates and a `loom.yaml` file that declares what to do with them — and then you run it whenever you need it. Loom renders your templates, writes them into a target Git repository, commits, pushes, and opens a pull request. One command, done.

```bash
loom run ./onboard-service -p serviceName=payments -p namespace=fintech
```

No scripting. No custom CI jobs. No imperative glue code. Just a declarative workflow that turns parameters into a pull request.

## The Problem

GitOps repositories accumulate operational patterns. Onboarding a service means creating an ArgoCD Application, an AppProject, a Gatekeeper constraint, and a kustomization entry — every time. Teams handle this in different ways:

- **Copy-paste**: fast, but drifts. Someone forgets a label, uses the wrong namespace, or misses the constraint file entirely.
- **Shell scripts**: better, but fragile. They grow organically, have poor error handling, and nobody wants to maintain them.
- **Internal platforms**: correct, but expensive. Most teams can't justify building and maintaining a self-service portal for what amounts to templating and a `git push`.

Loom gives you the structure of a platform without the cost. Your automation is version-controlled, composable, and runs from a single binary with no runtime dependencies.
