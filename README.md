# loom
Loom automates the last mile of your GitOps.

## Module System
In Loom, an repetitive automation is called a Module. You can create you own modules to define the GitOps automation process with an declarative approach.

Generally, an automation will include these operations:
1. git operations
    1. create new files in certain path
    2. patch existing files in certain path
    3. commit and push changes
    4. open PRs/MRs
2. execute external http calls
2. execute shell command

With Loom, you're able to create template of new files, patch existing files, and do everything above, by define an automation procedure file called `loom.yaml`.

### Structure of Module
A module is organized as a folder, that contains `loom.yaml` at the root of directory, new files template to be rendered and committed, and set of patches against existing files and http calls defined in yaml files.
```
.
├── argocd
│   ├── applicaton.yaml
│   └── project.yaml
├── cluster
│   └── constraints
│       └── pod-must-have-lable.yaml
└── loom.yaml
```

loom.yaml
```yaml


```
