# Quick start

The ordinary flow is deliberately small:

```text
configure
→ inspect a local or mounted server path
→ review the exact-storage plan
→ confirm and save
→ search, add Notes or tags
→ verify or restore when needed
```

The current browser flow accepts a path visible to the RestoreWeave host. A
future browser-upload flow may add a temporary upload stage, but it is not the
same thing as accepting a URL and it does not change the recovery model.

## Saving a file

1. Choose the configured storage scheme (the current profile is preselected).
2. Review the file count, logical bytes, exact duplicates, and any blocked
   entries.
3. Confirm the plan.
4. RestoreWeave stores or reuses the exact content and keeps the original path
   as provenance.

Optional analysis can run after the exact save. A model or index failure leaves
the saved bytes and keyword search usable.

## Finding a file

Search combines names, paths, metadata, Notes, tags, extracted text, and any
available semantic generation. A result always points back to the same stable
file subject; a similarity score never changes file identity or authorizes
deletion.

## Adding context

Use the single **Notes** surface for human notes and attributed imported,
extracted, or model-produced text. Add as many user tags as useful. System
facets such as file type are shown separately.
