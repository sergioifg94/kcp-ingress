# GLBC Release Process

### Lock main branch
- **Prerequisite**: You will need to be an admin in the kcp-glbc repository

In the repo settings page, on the Branches menu, edit the "main" Branch protection rules and apply the "Lock Branch" option and click Save.

### Run Smoke tests
Using the github smoke tests [action page](https://github.com/kcp-dev/kcp-glbc/actions/workflows/smoke.yaml) run the smoke tests using the main branch against both unstable and stable.

## Create release branch and tag

- **Prerequisite**: You will need a GPG signing key, and to have configured git to know about it:
  - [Generate GPG key](https://docs.github.com/en/authentication/managing-commit-signature-verification/generating-a-new-gpg-key)
  - [Add GPG key to github](https://docs.github.com/en/authentication/managing-commit-signature-verification/adding-a-gpg-key-to-your-github-account)
  - [Tell git about GPG key](https://docs.github.com/en/authentication/managing-commit-signature-verification/telling-git-about-your-signing-key)
- **Prerequisite**: Ensure you have pulled the most recent version of upstream/main, checked it out and have no uncommitted changes. 

Create the signed tag (replace x.y.z with the release version):
```
git tag --sign --message vx.y.z vx.y.z upstream/main
git push upstream vx.y.z
```

Create the release branch (this is only required when the release is a new minor release, i.e. 0.3):
```
git checkout -b release-x.y
git push upstream release-x.y
```

## Unlock main and inform the team
- **Prerequisite**: You will need to be an admin in the kcp-glbc repository

In the repo settings page, on the Branches menu, edit the "main" Branch protection rules and remove the "Lock Branch" option and click Save.