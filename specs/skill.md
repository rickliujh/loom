# Skill — Behavioral Specification

`loom skill` prints Loom's documentation for AI agents from the binary itself. An agent reads it into its context before working with modules, without anything having been installed.

```
loom skill [topic]
```

## Inputs

| Input | Description |
|-------|-------------|
| `topic` | Optional. A topic name, or the word `list`. |

## Behaviors

#### SK1: No argument prints the agent guide, then the topic index

`loom skill` writes the agent guide (`guide/ai-agents`) to stdout, followed by a `## More Topics` section listing every other topic with its title. The output opens with a comment naming the Loom version it describes.

Nothing else is written to stdout, and nothing to stderr: the output is meant to be read into a model's context as is. It ends with exactly one newline.

A documentation set without the agent guide cannot be served; loading it is an error.

#### SK2: The documentation ships inside the binary

Everything `loom skill` prints is embedded at build time from `docs/guide/*.md`, `docs/reference/*.md` and `specs/*.md`. The command reads no file, needs no module in the working directory, and makes no network request, so its output is the same wherever it runs and always matches the binary's own version.

Every Markdown file in those three directories is embedded. A build that carries no documentation fails with `this build of loom does not include its documentation`.

#### SK3: Topics are namespaced by where they come from

| Source | Topic name |
|--------|-----------|
| `docs/guide/<name>.md` | `guide/<name>` |
| `docs/reference/<name>.md` | `reference/<name>` |
| `specs/<name>.md` | `spec/<name>` |

A topic's title is its first level-one heading, read outside fenced code blocks and without any explicit heading id.

`loom skill list` prints one line per topic, sorted by name:

```
- `guide/ai-agents` — Using Loom with AI Agents
- `reference/op-patch` — patch
- `spec/smp` — Strategic Merge Patch (SMP) — Behavioral Specification
```

#### SK4: A topic is named in full, or by a bare name that is unambiguous

`loom skill <topic>` prints that one document. The name may carry a trailing `.md` or surrounding slashes.

A name without a namespace is accepted when exactly one namespace holds it:

```
loom skill op-patch      # prints reference/op-patch
loom skill llm           # error: guide/llm and spec/llm both match
```

On an error nothing is written to stdout.

#### SK5: Site-only markup is rewritten; everything else is left alone

The documents are written for the docs site. Outside fenced code blocks:

| In the source | Printed as |
|---------------|-----------|
| A link to another page, `[text](/reference/op-patch#smp)` | ``text (`loom skill reference/op-patch`)`` — the anchor is dropped, since a topic prints whole |
| A relative link between specs, `[smp.md](smp.md)` | ``smp.md (`loom skill spec/smp`)`` |
| `<code v-pre>…</code>` | `` `…` `` |
| An explicit heading id, `## Title {#id}` | `## Title` |

Left unchanged: links to a URL, same-page anchors, and any link whose destination is not a topic — a wrong command would be worse than a link the reader cannot follow. Fenced code blocks are never altered.

Every link between pages in the shipped documentation must resolve to a topic.

## Error Conditions

| Condition | Error |
|-----------|-------|
| No topic by that name | `unknown topic "<name>": run "loom skill list" to see the topics` |
| A bare name held by more than one namespace | `topic "<name>" is ambiguous: use one of <a>, <b>` |
| The build carries no documentation | `this build of loom does not include its documentation` |
| The documentation lacks the agent guide | `documentation is missing the agent guide "guide/ai-agents"` |
