# Terraform Plugin Protocol Buffers Stubs for Go

This repository contains Go packages that were automatically generated from
[Terraform's provider protocol definitions](https://github.com/hashicorp/terraform/tree/master/docs/plugin-protocol).

This is the lowest-level Go representation of the Terraform plugin protocol,
and as such it is rather inconvenient to use. These auto-generated stubs are
generally used only by SDK implementers who intend to wrap the low-level API
and provide a more convenient and idiomatic Go package API.

## Version Scheme

Each published version of the Terraform Plugin Protocol has a corresponding
Go Module release tag in this repository. The major and minor protocol versions
are mapped to major and minor module versions.

To use the stubs for the latest minor release of protocol version 5:

```
go get github.com/apparentlymart/terraform-plugin-proto-go/v5
```

You can also select a specific minor release, if necessary:

```
go get github.com/apparentlymart/terraform-plugin-proto-go/v5@v5.0
```

The version scheme describes breaking or non-breaking changes to the wire
protocol itself, and not necessarily to the generated Go packages. However,
as long as the Go code generator for protocol buffers remains
backward-compatible with earlier versions of itself the minor releases within
a particular major release should be backward-compatible from a Go API
standpoint too.

### Semantic Import Versioning

Because each major protocol release maps to a major release within this
repository, it's possible for a calling program to import two different major
versions at once.

This is important when a new major version is introduced so that plugin clients
and servers can support both versions for a time in order to make the transition
more graceful for users.

### Patch Releases

The Terraform protocol version scheme only includes major and minor versions.
The semver patch version number in this repository's release tags corresponds
to a _build number_ for a particular minor protocol release.

This is used to represent situations where the same upstream protocol buffers
file must be rebuilt for some reason, such as if it must be rebuilt with a
newer version of `protoc`.

It should always be safe to take the latest patch release for a given minor
release. For most minor releases there will only be a single patch number zero.

## Licence

The Go code in this repository is mechanically derived from the protocol
definitions in the main Terraform repository, and thus it is not a creative
work in its own right and does not imply any additional copyright.

For licensing information regarding the original protocol definitions, consult
[the license for Terraform itself](https://github.com/hashicorp/terraform/blob/master/LICENSE).
