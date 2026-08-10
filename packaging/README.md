# Cutting a release

```
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin vX.Y.Z
```

`.github/workflows/release.yml` does the rest. The tag must be on a clean tree:
`make dist` takes the version from `git describe --tags --always --dirty`, and the
workflow's version check fails on a `-dirty` build rather than shipping it under the
tag's name.

## By hand

```
make dist
sh packaging/formula.sh vX.Y.Z dist > dist/urd.rb
brew install --build-from-source ./dist/urd.rb
```

To try the formula with no published release, point a `url` at
`file:///absolute/path/to/dist/urd_....tar.gz` first.

The formula also goes to `ristir/homebrew-tap`, shared by every project.
