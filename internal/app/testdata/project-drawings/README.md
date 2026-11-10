# Synthetic project drawings fixture

All identities, geometry, SVG metadata and PNG pixels are handwritten test data.
No customer project, library asset, private capture or live EDA acceptance is represented.
The deliberately identical PNG files test byte-preserving packaging across roles;
the actual-render SVGs are metadata stand-ins for parser tests, not renderer acceptance.

Run `go test ./internal/app -run TestProjectDrawings`.
For a disposable preview, use `easyeda project drawings --from internal/app/testdata/project-drawings/spec.json --out /existing-parent/new-drawings-directory`.
The output directory must not already exist.
