[![Build Status](https://travis-ci.org/gadumitrachioaiei/goequal.svg?branch=master)](https://travis-ci.org/gadumitrachioaiei/goequal)
[![Go Report Card](https://goreportcard.com/badge/github.com/gadumitrachioaiei/goequal)](https://goreportcard.com/report/github.com/gadumitrachioaiei/goequal)

`goequal` generates equal like functions for a given type. It reads a type recursively and generates a function for each named type refered by the original type. The generated code is written in a file per named type in the directory package.

Example:
-------
```

type Y struct {
    a int
}

type X struct {
    a Y
    b map[int]int
}
```

Generated code will be: 
```

func EqualY(t1, t2 *Y) bool {
    if t1 == t2 {
        return true
    }
    if t1 == nil || t2 == nil {
        return false
    }
    if t1.a != t2.a {
        return false
    }
    return true
}

func EqualX(t1, t2 *X) bool {
    if t1 == t2 {
        return true
    }
    if t1 == nil || t2 == nil {
        return false
    }
    if !EqualY((&t1.a), (&t2.a)) {
        return false
    }
    if len(t1.b) != len(t2.b) {
        return false
    }
    for key1, value11 := range t1.b {
        if value12, ok := t2.b[key1]; !ok {
            return false
        } else {
            if value11 != value12 {
                return false
            }
        }
    }
    return true
}
```

Calling the generator ( it is necessary to first install your package: https://github.com/golang/go/issues/14496 ):
---------------------
    $ go install packagePath
    $ goequal -type typeName -package packagePath

Reason:
-------

As opposed to standard library reflection the generated code runs much faster.
And of course there is the added benefit of not writing the code yourself.

The code is generated using these rules:
---------------------------------------
1. For struct types we evaluate the equality for pointers to struct.
2. We evaluate private variables.
2. Named types from standard library are ignored.
3. Interfaces are evaluated using reflect.DeepEqual
4. Channels and function types are ignored.
5. Two slices of bytes are evaluated to be equal using bytes.Equal from standard library.
6. Two slices are considered equal if they have the same length and the same element for each index.
7. Two maps are considered equal if they have the same length, same keys and same values for each key.
8. Two pointers are equal if they are equal as pointers or if the values they point to are equal.

TODO:
----
1. Offer the possibility to call custom functions to evaluate equality for two types or variables. For examples, one might want to evaluate a certain slice equality independent of order of its elements.
2. Offer the possibility to ignore private fields, or other types or variables.
