# Benchmarks behind decision D1

Measured on 2026-10-28 and 2026-10-29. Machine: Apple M3, 8 cores, 16 GB,
macOS 26.6. Go 1.27.3, Rust 1.96.1, MyGo v0.7.3, Gio v0.10.3, GPUI
(gpui-pre) 0.3.9. Other processes ran on the Mac during the runs (load
average 2–5), and the runs within each table shared those conditions.

## Grid test (runtime)

One window shows a 200×60 grid of colored text runs, like a terminal pane.
Three modes:

- **idle:** the grid does not change.
- **scroll:** one new line per frame at 60 updates per second, like build
  output.
- **churn:** every cell changes on every frame, the worst case.

CPU is from `ps`. Memory is `phys_footprint` from `footprint`. Each app ran
alone, with its window on screen.

| | GPUI (Rust) | MyGo native (Go) | Gio (Go) |
|---|---:|---:|---:|
| Idle: CPU / memory | 0.5 % / 77 MB | 0.0 % / 67 MB | 0.0 % / 289 MB |
| Scroll: CPU / memory / fps | 29 % / 159 MB / 56 | 28 % / 190 MB / 60 | 31 % / 405 MB / 49 |
| Churn: CPU / memory / fps | 47 % / 159 MB / 54 | 64 % / 200 MB / 60 | 110 % / 436 MB / 26 |
| Scroll with the app hidden: CPU | 1 % | 3–4 % | 32 % |

The test apps paint the way the Rust contr terminal paints: one shaped
text run per style run, and a rectangle for each background. Gio draws
glyphs as vector paths and shapes text in pure Go. A profile of its churn
mode showed glyph outlines (about 32 %), shaping (about 22 %) and buffer
uploads (about 15 %). MyGo and GPUI draw glyphs from an atlas. The rest of
the gap between MyGo and GPUI in churn is probably MyGo's pure-Go shaping
against GPUI's CoreText. This cause is not measured.

The MyGo test used the system UI font, not a mono font.

## Builds

`/usr/bin/time -l`. CPU is user plus system time of the build and its child
processes. "Cold" means an empty target folder or build cache.

| | Rust: gpui-only crate | Go: MyGo | Go: Gio |
|---|---:|---:|---:|
| Cold: wall / CPU | 39 s / 185 CPU-s | 8.8 s / 34 CPU-s | 9.7 s / 34 CPU-s |
| Edit: wall / CPU | 0.7 s / 1.3 CPU-s | 0.4 s / 0.9 CPU-s | 0.8 s / 1.1 CPU-s |
| Disk | 1.6 GB | 0.19 GB | 0.1 GB |

Four cold builds at the same time: Rust took 106–146 s for each, with load
average 16 and about 60 MB of free memory. Go (Gio) took 26 s for each.

Rust contr itself (43,600 lines), for scale: an edit plus a build took
1.4 s and 2.4 CPU-s. An edit plus a test build took 2–3.5 s and 6–15 CPU-s.
With four of those at the same time, each took 7–14 s. The cargo processes
waited on cargo's shared package cache lock.

## Terminal painter (M5.4)

Measured on 2026-10-29, same machine, one run at a time, 15 samples of `ps`
CPU over 15 s after a 6 s warm-up. The earlier MyGo test was run again in the
same session, so both columns share the machine load. The painter draws the
same 200×60 workload from an `endpoint.Surface`, with the bundled JetBrains
Mono (the earlier test used the system UI font). Cell size 7.2×15.8 so the
grid fits the same window.

| | MyGo test (rerun) | Terminal painter |
|---|---:|---:|
| Idle: CPU / memory | 0.0 % / 67 MB (table above) | 0.0 % / 83–87 MB |
| Scroll: CPU / memory / fps | 23 % / 195 MB / 60 | 19–24 % / 230 MB / 60 |
| Churn: CPU / memory / fps | 68 % / 200 MB / 60 | 29–51 % / 78–195 MB / 60 |
| Scroll with the app hidden: CPU | not rerun (3–4 % above) | 1–2 % |

CPU is at or under the MyGo numbers. Memory is 20–35 MB higher at idle and in
scroll; the cause is not measured (the registered font faces and the glyph
atlas for them are the likely part). The painter keeps shaped glyphs per row
(by row identity) and by content, so text that moves to another row, as in a
scroll, is not shaped again. Without the content memo, scroll took 48 % CPU.

## How to run the grid test again

The terminal painter test is `cmd/gridbench` (build tag `bench`). Run it from
the repository root, one mode at a time, and read CPU with `ps`:

    go build -tags bench -o tmp/gridbench ./cmd/gridbench
    tmp/gridbench -mode idle|scroll|churn

`-mode sample` shows a fixed surface (colors, box glyphs, underline styles,
selection, cursor). Build it with `-tags bench,e2e` to capture the window with
MyGo's automation plugin.

The earlier MyGo, Gio and GPUI programs are not in this repository.
