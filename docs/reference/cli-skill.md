# loom skill

Print Loom's guide for AI agents, straight from the binary.

```
loom skill [topic]
```

An agent that is about to author, validate or run a module needs to know how
Loom behaves. `loom skill` hands it that knowledge in one command: the guide is
embedded in the binary, so there is nothing to install, nothing to fetch, and
the text always describes the version of Loom that is about to run.

## Usage

| Command | Prints |
|---------|--------|
| `loom skill` | The [agent guide](/guide/ai-agents), followed by the list of topics. |
| `loom skill list` | The topics alone, one per line with its title. |
| `loom skill <topic>` | One topic. |

The output is Markdown on stdout and nothing else, so it can be read into a
model's context as is.

## Topics

Every page of this documentation and every behavioral spec is a topic:

| Namespace | Source | Example |
|-----------|--------|---------|
| `guide/` | The guide pages | `loom skill guide/templates` |
| `reference/` | The reference pages | `loom skill reference/op-patch` |
| `spec/` | The behavioral specs | `loom skill spec/smp` |

The namespace can be left off when the name is unambiguous — `loom skill op-patch`
prints `reference/op-patch`. A name that more than one namespace holds, such as
`llm`, is refused with the candidates listed.

## Pointing an Agent at It

Add one line to whatever your agent reads on startup — `AGENTS.md`, a rules
file, or a skill:

```markdown
Before working with Loom modules, run `loom skill` and follow what it prints.
```

That line never goes stale. The guide it leads to is updated with Loom itself.

## How the Text Differs from This Site

Links between pages are rewritten into the command that prints the destination,
since a reader of the binary has no site to click through:

```
See the patch reference (`loom skill reference/op-patch`).
```

Links that leave the documentation are kept as they are, and code blocks are
never altered.
