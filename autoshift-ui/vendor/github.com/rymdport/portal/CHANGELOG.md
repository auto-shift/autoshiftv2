# Changelog

The version of this module's API is still in a `v0.X.Y` state and is subject to change in the future.
A release with breaking changes will increment X while Y will be incremented when there are minor bug or feature improvements.

## v0.4.0

- Rename the `Writeable` field in `openuri.OpenURIOptions` to `Writable` to fix spelling error.
- Implement the `OpenFile` and `OpenDirectory` functions in the `OpenURI` protocol.
- Implement the `Trash` protocol.
- Add some new helpers to the `settings/appearance` package to convert `any` values to corresponding values. Useful inside the change listener of the `settings` package.