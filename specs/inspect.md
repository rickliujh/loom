# Inspect — Behavioral Specification

This document describes the expected behavior of the **`inspect`** command, which describes a loom module and every module it composes without executing any of it.

Inspect answers three questions about a module you are about to run: what it is made of (the modules it composes), what it will do (its operations, in execution order), and what you must supply for it to run (its parameter requirements).

Modules inspected conform to the module specification defined in [`specs/module.md`](module.md).

## Inputs

| Input | Description |
|-------|-------------|
| `source` | Optional. Module path or git URL, resolved exactly as `run` resolves it (including the `//subdir` form). Defaults to `.` (current directory). |
| `params` | Optional. `map[string]string` supplied for the root module, as `run` accepts them. |
| `maxDepth` | Optional. Levels of module to describe. `1` is the subject alone; `0` or less means all of them. Defaults to `1`. |
| `module` | Optional. Instance name, or `/`-separated path of instance names, selecting which module is described. Defaults to the root. |
| `noFetch` | Optional. When set, modules whose source is a git URL are listed but not cloned. Defaults to false. |
| `output` | Optional. `tree` (default) or `json`. |

The module being described is the **subject**. It is the root unless `module` selects another.

---

## Side Effects

#### IN1: Inspect executes nothing

Inspecting a module runs none of its behavior:

- No operation executes, in any module in the tree.
- No `dynamicParams` command runs. The command is reported as authored, and the parameter has no value.
- No `if` condition is evaluated, on either an operation or a submodule. The condition is reported as authored, and the module or operation it gates is described either way — inspect shows what *could* run, not what *would*.
- No target repository is cloned. A module's `target` is reported, not fetched.

The single side effect inspect permits is cloning a module `source` that is a git URL, which is unavoidable if its contents are to be described. Those clones go to temporary directories and are removed before inspect returns; with `noFetch` they do not happen at all.

---

## Module Hierarchy

#### IN2: The tree follows execution order

Submodules are reported in the order `spec.modules` declares them — the order a run dispatches them in — and each module's own operations in the order `spec.operations` declares them. Order is a property of the module being described, never re-sorted for presentation.

#### IN3: Modules are identified by instance name

Each module in the tree is identified by the name the parent's `modules[].name` gives it, rendered against the parent's resolved params. This is the identity a run logs and heads diffs with, and it is what distinguishes two instances of the same source. The module's own `metadata.name` is reported alongside it when the two differ.

At the root, where no parent names the module, the instance name is the module's `metadata.name`.

#### IN4: A cycle stops the walk

A module whose resolved source already appears among its own ancestors is reported as a cycle and not expanded. Source identity is the absolute directory for a local source and the URL for a remote one.

A module appearing more than once in the tree without being its own ancestor is not a cycle — it is composed twice — and is described in full at each position.

#### IN5: Modules past the depth limit are listed, not dropped

A module beyond `maxDepth` is still reported, carrying what its parent declares about it — instance name, source, and `if` condition — and marked as **listed**: named, but not read. Its parameters, operations, and submodules are unknown, because its config was never opened.

A listed module is never resolved, so a listed remote module is never cloned. This is what makes a shallow inspection cheap.

Reaching the depth limit is not an error.

#### IN16: One module is described by default

`maxDepth` defaults to `1`: the subject is described, and the modules it composes are listed. This answers the common question — what is this module, and what does it need — without reading a tree that may span several repositories.

Describing more is explicit: raising `maxDepth`, or setting it to `0` for the whole tree.

#### IN17: Any module in the tree can be made the subject, and more than one

`module` selects which module is described. It matches against the tail of a module's instance breadcrumb, so a bare name selects that module wherever it sits, and a `parent/child` path distinguishes modules that share a name.

Matching anything other than exactly one module is an error naming the candidates — a query that silently picked one of several would describe the wrong module. Each subject is reported with its breadcrumb from the root, so its position in the tree is never in doubt.

`module` may be given more than once, describing several modules in one report — two siblings compared side by side, say. The subjects are reported in the order they were requested, and naming one twice describes it once. Subjects are independent of each other: two can overlap, one sitting inside another, and applying the depth limit to one never reduces what is reported for the other.

The roll-ups of IN9 span every subject as one set, deduplicated, because what the caller must supply is a single list regardless of how many modules they asked to see.

Selecting a module requires finding it, so the tree holding it is read regardless of `maxDepth`; the limit then applies to each subject and what it composes.

Parameters are resolved along the way down, exactly as for an unfocused inspection: a subject deep in the tree shows the values its parents actually hand it, not defaults it would never see.

---

## Parameters

#### IN6: Every declared parameter is reported with the origin of its value

For each module, both `spec.params` and `spec.dynamicParams` are reported, each in one of these states:

| State | Meaning |
|-------|---------|
| `provided` | A value was supplied — from the CLI at the root, or from the parent's `modules[].params` for a child. |
| `default` | Nothing was supplied, so the declared `default` applies. |
| `dynamic` | The value comes from a `dynamicParams` command at run time. Inspect reports the command, not a value. |
| `missing` | Required, with no default and nothing supplying it. |
| `unset` | Optional, with no default and nothing supplying it; it renders as the empty string. |
| `unresolved` | Supplied by the parent, but through an expression the parent cannot resolve statically. |

Resolution mirrors the module spec: a supplied value wins over a default (P1), and a supplied value also wins over a `dynamicParams` command (P6), so a dynamic parameter that was supplied is reported as `provided`.

#### IN7: Parent-supplied values are traced to the expression that produced them

A parameter a parent supplies through `modules[].params` is reported with that expression as authored alongside its rendered value, so a templated hand-off stays traceable to its source.

#### IN8: Templates that need run-time values are reported as unresolved, never as a value

Inspect renders templates against only the parameters whose values it actually knows — those in state `provided` or `default`. A template referencing anything else (a dynamic parameter, a missing one) cannot be resolved:

- A `modules[].params` value that fails to resolve makes that child parameter `unresolved`, with no value. A placeholder is never passed down as if it were a real value.
- A `modules[].source` that fails to resolve is an error on that child, and the walk does not descend into it — the module it names is not knowable.
- A `target` field that fails to resolve keeps its template text.

#### IN9: Missing required parameters are collected across everything described

Every parameter in state `missing`, in the subject or any module described beneath it, is collected into one list, each located by the instance breadcrumb of the module that declares it. This is the set of values a run would fail without, and in a composed tree the module that needs a value is frequently not the one being invoked.

The claim is scoped to what was actually read. Listed and unfetched modules may require parameters of their own, so while any exist they are reported alongside, and the result is never presented as a complete account of the tree's requirements.

Missing parameters do not make inspect fail: reporting them is what the command is for.

#### IN10: Parameters supplied but not declared are reported as warnings

A parameter a parent passes that the receiving module does not declare is reported as a warning on that module. A run rejects this outright (P3); inspect reports it and carries on, so the whole tree is still described.

---

## Operations

#### IN11: Each operation is reported with its name, action kind, and a summary

Every entry in `spec.operations` is reported with its name, its action kind (`newFiles`, `patch`, `shell`, `commitPush`, `pr`, `llm`), a short summary of what it acts on, and its `if` condition when it has one. Operation summaries are derived from the same classification the executor uses to build actions, so an action kind cannot be described one way and run another.

Templates inside an operation are reported as authored. An operation's rendering depends on run-time state (the target working tree, dynamic params) that inspect does not have.

#### IN12: An operation is never silently dropped

An operation declaring no recognized action type — the failure `FromOperation` raises at run time — is rejected by structural validation, so the module carrying it is reported as an error (IN14) naming that operation. The failure is never hidden by quietly omitting the operation from the description.

Should such an operation reach the describing step anyway (a caller using the package without validating first), it is listed with the error on it and counted as a problem, rather than skipped.

---

## Failure Handling

#### IN13: A failure below the root is contained to its node

A submodule that cannot be resolved, loaded, or structurally validated is reported with its error in place, and the walk continues with its siblings. One broken submodule never hides the rest of the tree.

A root that cannot be described is a hard failure: there is no tree to report.

#### IN14: Structural validity fails a module; missing files only warn

A module whose config fails structural validation is reported as an error, and its contents are not described — its parameters and operations cannot be trusted. A module that passes structural validation but fails the filesystem checks (a `newFiles.source` directory or `patch.path` file that is not present) is described in full, with those findings as warnings: they are real, but they do not make the description untrustworthy.

#### IN15: Exit status reflects describability, not runnability

Inspect exits non-zero when any module in the tree could not be described. It exits zero otherwise — including when required parameters are missing, which is a fact about a prospective run rather than a failure of the inspection.
