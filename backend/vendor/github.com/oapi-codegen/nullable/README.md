# oapi-codegen/nullable

> An implementation of a `Nullable` type for JSON bodies, indicating whether the field is absent, set to null, or set to a value

Unlike other known implementations, this makes it possible to both marshal and unmarshal the value, as well as represent all three states:

- the field is _not set_
- the field is _explicitly set to null_
- the field is _explicitly set to a given value_

And can be embedded in structs, for instance with the following definition:

```go
obj := struct {
		// RequiredID is a required, nullable field
		RequiredID     nullable.Nullable[int]     `json:"id"`
		// OptionalString is an optional, nullable field
		// NOTE that no pointer is required, only `omitempty`
		OptionalString nullable.Nullable[string] `json:"optionalString,omitempty"`
}{}
```

## Usage

> [!IMPORTANT]
> Although this project is under the [oapi-codegen org](https://github.com/oapi-codegen) for the `oapi-codegen` OpenAPI-to-Go code generator, this is intentionally released as a separate, standalone library which can be used by other projects.

First, add to your project with:

```sh
go get github.com/oapi-codegen/nullable
```

Check out the examples in [the package documentation on pkg.go.dev](https://pkg.go.dev/github.com/oapi-codegen/nullable) for more details.

## Credits

- [KumanekoSakura](https://github.com/KumanekoSakura), [via](https://github.com/golang/go/issues/64515#issuecomment-1842973794)
- [Sebastien Guilloux](https://github.com/sebgl), [via](https://github.com/sebgl/nullable/)

As well as contributions from:

- [Jamie Tanna](https://www.jvt.me)
- [Ashutosh Kumar](https://github.com/sonasingh46)

## License

Licensed under the Apache-2.0 license.
