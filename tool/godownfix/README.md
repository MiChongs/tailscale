# godownfix

Type-aware rewrite of Go 1.26+ constructs to Go 1.25 equivalents, used after
rebasing onto a new upstream tag:

    cd tool/godownfix && go build -o /tmp/godownfix . && cd ../..
    for os in linux windows darwin freebsd openbsd android; do
      GOTOOLCHAIN=local GOOS=$os CGO_ENABLED=0 /tmp/godownfix ./...
    done

It rewrites `new(expr)`, `errors.AsType`, `reflect.Type/Value` iterators and
`netip.Prefix.Compare` calls onto `internal/godown/std`. Method expressions
(`netip.Prefix.Compare` as a value) and unused imports left behind are fixed by
hand; verify with `GOTOOLCHAIN=go1.25.5 go build ./...` for every GOOS.
