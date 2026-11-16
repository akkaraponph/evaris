# Evaris CLI

Evaris evaluates agent and skill outcomes with reproducible experiments and evidence. See the [repository](https://github.com/refactorroom/evaris) for documentation.

Run `npx evaris-cli@latest --version` to use the latest npm release. Install or update a global installation with `npm install -g evaris-cli@latest`, then run `evaris --help`. Node.js 18 or newer is required. Linux and macOS on x64 or arm64 are supported.

The npm package includes native binaries; it does not download code when run. An `update` subcommand is not provided. For a project-local installation use `npm install --save-dev evaris-cli@latest`. Keep existing evidence when changing versions; older Evaris versions may reject records created with newer schemas.
