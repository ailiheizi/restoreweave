# Index status and search

RestoreWeave treats analysis as an optional enhancement to exact storage.

## The small baseline

- **Keyword and structured search** cover names, paths, metadata, Notes, tags,
  checksums, duplicate information, and processing state.
- **BGE text search** uses the pinned local `BAAI/bge-small-zh-v1.5` profile
  when its verified model and vector generation are available. It may be built
  in the background or on demand; it never blocks exact saving.
- **One search box** can combine the available dimensions and merge results by
  stable file subject.

## Future media indexes

Image CLIP/SigLIP and music-feature models should be separate index dimensions:

```text
BGE → text and Notes
CLIP/SigLIP → image content
music model → audio features
```

Each dimension has its own model, vector space, and generation. A unified
search entry may fuse their subject-level results, but vectors are never mixed
as if they were the same space. These media dimensions are not current default
capabilities.

## What a file can show

The detail view can report each eligible dimension as:

```text
ready · queued · not built · unavailable · not enabled · not applicable
```

“Not applicable” is used for an image index on a text file, for example. A
missing or failed optional index is visible but does not make exact content
unreadable.

## System filters are not user tags

The interface may offer filters such as **no user tag** and **index not ready**
beside the tag controls. They are calculated system state, not durable
`Annotation.TAG` rows. This keeps user organization clean while still making
unfinished analysis easy to find.
