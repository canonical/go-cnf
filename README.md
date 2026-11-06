
# Go support for the _cnf_ format


## Packages

- [go-cnf/cnfast](https://pkg.go.dev/github.com/canonical/go-cnf/cnfast) - Original AST parser and renderer built during spec development. 
- [go-cnf/cnfpretty](https://pkg.go.dev/github.com/canonical/go-cnf/cnfpretty) - Pretty printer for cnfast. 
- [go-cnf/cnfjson](https://pkg.go.dev/github.com/canonical/go-cnf/cnfjson) - Text-preserving bridge between cnf and json. 
- [go-cnf/cnfembed](https://pkg.go.dev/github.com/canonical/go-cnf/cnfembed) - Text-preserving cnf to json, self-contained file for embedding.
- [go-cnf/cnfmap](https://pkg.go.dev/github.com/canonical/go-cnf/cnfmap) - Simple Marshaler and Unmarshaler for Go values. 
- [go-cnf/cnftest](https://pkg.go.dev/github.com/canonical/go-cnf/cnftest) - Harness to run the conformance tests.


## Specification

Consult the [full specification](https://github.com/canonical/cnf) for examples and details of the _cnf_ format.


## Performance

The following table offers a vague data point on the performance of the
implementation. It's as fair as I could make it: the two documents spell
the same data in the same indentation shape. Both documents decode to the
identical tree: 6721 maps, 11833 lists, 24605 scalars.

| Test                                     | Parser                   | Best (ms) |  MB/s |
|------------------------------------------|--------------------------|----------:|------:|
| yaml => map (`yaml.Unmarshal`)           | gopkg.in/yaml.v3         |      45.2 | 23.79 |
| cnf => json (`ToJSON`)                   | cnfembed                 |      18.3 | 88.06 |
| cnf => map (`ToJSON` + `json.Unmarshal`) | cnfembed + encoding/json |      27.0 | 59.78 |


## Example

This simple example shows the conversion of cnf to json and back:

```cnf
key: "value"
nested.key: 0xff
string:
    """
    Hello
    world
    """
```

One call decodes it to JSON:

```go
json, err := cnfjson.ToJSON(doc, nil)
```

This results in:

```json
{"key":"value","nested":{"key":255},"string":"Hello\nworld\n"}
```

One call converts it back:

```go
cnf, err := cnfjson.FromJSON(json, nil)
```

The value is preserved, but the spelling changed slightly as it
lacks the metadata:

```cnf
key: "value"
nested:
    key: 255
string:
    '''
    Hello
    world
    '''
```

For more careful transformations and pretty printing, see `cnfpretty`.
