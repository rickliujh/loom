I am building a devops gitops automation tool called loom. The interface is yaml files, using it to orchestrate 3 types operations described in the README.md.

In the high level, it should be modules, a module defines a set of operations that should be performed together, modules can be combined and orchestrated by root level module. Module has below functions:

1. module can be referenced in local path, or a remote git repository. 
2. Each module has a loom.yaml file at it's root, this file defines all the tasks and the order to execute them. If loom.yaml is not found, it should use loom.jsonnet to generate the loom.yaml file for more advanced
orchestration. 
3. loom.yaml schema should follow k8s yaml schema, with apiVersion and Kind in the beginning to signal the use of the yaml file.
4. Module can declare parameters, that will be passed in during the execution by user or be passed by a module that referenced it.

In operation level, there is a few requirements:

1. new file on target git repo
    1. new file operation should use the file templates in the module root directory where loom.yaml or loom.jsonnet resides.
    2. the file template should maintain the directory structure as they should in the target git repo.
    3. before apply all templates will be rendered and apply to a git repository
    4. parameters for the rendering should be given or declared on loom.yaml
2. patch file on target git repo
    1. patch should support kustomize files, but also leave room for the future to support yq or josnnet
    2. patch can use parameters declared on loom.yaml
3. Commit&push a repo
    1. Commit should add all changes before committing
    2. Commit should be able to set correct message, user,and email
    3. Commit can use parameters declared on loom.yaml
    4. Push pushs all commit to remote repo, no difference compared to git push
4. PR/MR
    1. PR/MR opens a pull request or merge request to github or gitlab, respectively
    2. PR/MR should be able to set the metadata for that PR/MR
    3. PR/MR can use parameters declared on loom.yaml
5. HTTP calls (don't implement it this time)
6. Shell command
    1. Shell command can use parameters declared on loom.yaml
    2. Shell command should be able to declare the metadata (name, timeout, etc.)
    3. Shell command should be able to declare the command to run on host machine

more inforamtion on readme @README.md

High level diagram: ./high-level-diagram.png

uses golang to implement
uses cobra for cli framework



