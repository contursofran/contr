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

## Whole app (M13)

Measured on 2026-10-29 with `cmd/contr` (build tag `e2e`) against the dev
herdr, on `main` 26b904b plus the log line in d823bb0. Same machine as
above. Three runs, one at a time; the table is the median run (by idle
shown CPU). Other processes ran on the Mac (load average 1.7–3.8, shown per
phase), as in the earlier tables. Method: 6 s warm-up, 15 samples, one per
second, from `ps` and `footprint`. The window was hidden with the menu
contr > Hide. A window that other windows cover was not measured (D9).
The terminal pane is 720×834 points, smaller than the 200×60 cells of the
grid test, so these numbers are not the same workload. The board shows a
working worker (an animated arc).

| Phase | CPU min / med / max | Footprint max | RSS max | Load avg |
|---|---|---|---|---|
| idle shown | 11.5 / 18.3 / 22.6 % | 218 MB | 168 MB | 2.47 2.71 3.21 |
| busy pane shown | 12.2 / 16.2 / 18.5 % | 180 MB | 194 MB | 2.41 2.68 3.19 |
| flood pane shown | 10.1 / 10.7 / 12.0 % | 188 MB | 214 MB | 2.06 2.57 3.14 |
| board updates shown | 16.5 / 23.4 / 27.4 % | 187 MB | 214 MB | 2.16 2.56 3.12 |
| idle hidden | 0.0 / 0.0 / 0.5 % | 80 MB | 215 MB | 2.00 2.49 3.08 |
| busy pane hidden | 1.9 / 2.0 / 5.7 % | 82 MB | 215 MB | 1.68 2.37 3.01 |

RSS max is a high-water mark from the whole run, so it does not fall when
the window is hidden. Use the footprint column for memory.

Targets (a miss is reported only):

- idle shown: not met. CPU 18.3 % against 0.5 %. Footprint 218 MB against
  the 150 MB flag.
- busy pane shown: met. CPU 16.2 % against 24 %; 180 MB against 230 MB.
- flood pane shown: met. CPU 10.7 % against 51 %; 188 MB against 230 MB.
- board updates shown: not met. CPU 23.4 % against 2 %. Footprint went
  from 184 MB to 187 MB across the phase (run 1: 187 to 170, run 2: 183 to
  186), so there is no growth.
- idle hidden: met. CPU 0.0 %.
- busy pane hidden: met at the limit. CPU 2.0 % against 2 %. Runs 1 and 2
  gave 2.1 %, so 2 of 3 runs were over the target.

Run to run, the other phases changed by less than 3 points. Board updates
shown gave 10.8 % in run 1 and 21.7–23.4 % in runs 2 and 3.

Likely cause of the misses (read from the code, not profiled; M13.4 below
proves it): the working arc in the board panel redraws every frame
(`icons.go`, `AnimationFrame`), about 60 frames per second while the window
is shown. Hidden, the frames stop and CPU is 0.0 %. Memory: likely, not
profiled at M13.3; M13.4 below has a `footprint` breakdown.

### Working arc at 30 frames a second (M13.4)

Measured on 2026-10-29, same machine and method, one run each, on `main`
a7895f2 (before) and on the M13.4 branch (after). The display refreshes
at 60 Hz. Load average 1.9–4.6.

Proof of the cause. With `MYGO_FRAME_STATS=all`, the idle shown phase drew
964 frames in 15 s (64 a second), and 956 of them were "painted again"
(no view build) with a redraw of 880×96 pixels, the arc's row. Each frame
took about 0.5 ms on the main thread. A test build with the arc's frame
request removed gave idle shown 0.0 % and board updates shown 0.3 %. So
the arc is all of the idle CPU, and the board updates cost almost nothing
alone (about one view build a second, 1–2.6 ms, only the changed parts
redrawn).

The fix: the arc asks for frames with `AnimationFrameRate(30)` and turns
in 42 steps of 1/30 s (`theme.SpinFPS`, D26). Rust (GPUI
`repeat_synced`) draws the arc at the display's rate.

| Phase (CPU median) | Before | After, 30 a second | Test, 15 a second |
|---|---|---|---|
| idle shown | 15.9 % | 10.1 % | 5.1 % |
| busy pane shown | 16.2 % | 11.5 % | 7.7 % |
| flood pane shown | 10.8 % | 5.9 % | 3.1 % |
| board updates shown | 19.2 % | 12.6 % | 6.3 % |
| idle hidden | 0.0 % | 0.0 % | 0.0 % |
| busy pane hidden | 1.9 % | 1.9 % | 1.7 % |

Footprint max, before / after: idle shown 218 / 210 MB, hidden 82 / 81 MB.
The frame log of the after build shows 483 frames in 15 s (32 a second).

The CPU falls by less than the frames: about 0.3 % for each frame a
second. MyGo logs about 0.5 ms of it; `sample` shows the threads waiting
in the kernel (`__psynch_cvwait`, `mach_msg2_trap`, `__workq_kernreturn`)
and almost no contr code. So most of the cost of a frame is likely in the
system's present path (not profiled further). With a working item on
screen, idle shown cannot meet 0.5 % at any rate that still spins; with
nothing working, idle shown is 0.0 %.

Memory at idle shown (`footprint`, 204 MB): IOSurface 59 MB in 4 regions
(the window's drawables: one at 2628×1645 pixels is 17 MB), graphics
memory 56 MB and IOAccelerator 8 MB (GPU textures, likely the glyph atlases
and scene buffers), untagged VM 54 MB (likely the Go heap), malloc 12 MB.
Hidden, the surfaces go and the footprint is about 81 MB. Likely, not
profiled further.

Screenshots (`tmp/bench/start.png`, `busy.png`) show the sidebar, the grid
with the shell prompt, the board, and the busy pane scrolling.

### Frames and CPU seconds per phase (M13.6)

CPU percent on a shared Mac moves with other load: idle shown read 3.4,
5.3, 18.3 and 15.9 % in four runs. Frames and CPU seconds depend much less
on load. **Compare runs by frames and CPU seconds**, not by CPU percent.
The test now records, for each phase:

- Frames: the lines of MyGo's frame log (`MYGO_FRAME_STATS=all`, which the
  test sets for the app) between the phase's "samples start" and "samples
  end" marks in `tmp/bench/app.log`, and frames a second (frames divided by
  the time between the marks, about 15 s).
- CPU s: the CPU time of the app process (user plus system, from `ps -o
  time=`, 10 ms steps) at the end of the phase minus the value at the start.
- Working: whether a working item (the spinning arc) is on screen. The
  fixture row has its working stage on or off; the test sets it. Every
  phase has nothing working, except "idle shown, one item working", which
  measures the cost of the arc.

The CPU percent columns and the targets list stay; a miss is reported only.

Owner decision D4: the arc paints 30 times a second, and the idle target
(0.5 %) is measured with nothing working. So "idle shown" has nothing
working. Tables from M13.3 and M13.4 above had a working item in every
shown phase, so their shown phases are not comparable with the new
"nothing working" phases; the new "idle shown, one item working" phase is.

`tmp/bench/result.json` has the same numbers for a later CI job to compare
with a baseline from the same runner. It is one object: the phase name to
an object with these keys:

| Key | Meaning |
|---|---|
| `frames` | frames drawn in the phase |
| `frames_per_s` | frames divided by the seconds of the phase |
| `cpu_seconds` | CPU seconds (user plus system) the app used in the phase |
| `cpu_med` | median CPU percent of the samples |
| `footprint_max` | largest footprint in MB |
| `working` | true when a working item was on screen |

Example: `{"idle shown": {"frames": 11, "frames_per_s": 0.693,
"cpu_seconds": 0.06, "cpu_med": 0, "footprint_max": 202, "working": false}}`.

## How to run the grid test again

The terminal painter test is `cmd/gridbench` (build tag `bench`). Run it from
the repository root, one mode at a time, and read CPU with `ps`:

    go build -tags bench -o tmp/gridbench ./cmd/gridbench
    tmp/gridbench -mode idle|scroll|churn

`-mode sample` shows a fixed surface (colors, box glyphs, underline styles,
selection, cursor). Build it with `-tags bench,e2e` to capture the window with
MyGo's automation plugin.

The earlier MyGo, Gio and GPUI programs are not in this repository.

## How to run the app test

The app test (M13) measures the whole app in seven phases: idle shown, idle
shown with one item working (M13.6), busy pane shown, flood pane shown, board updates shown, idle hidden, busy pane
hidden. Each phase has a 6 s warm-up and 15 samples, one per second. It
runs only against the dev herdr, with a scratch repo and a scratch board.

    eval "$(scripts/dev-herdr.sh)"
    go test -tags e2e,bench -run AppBench -v -timeout 15m ./cmd/contr
    scripts/dev-herdr.sh stop

One heavy job at a time: run nothing else (e2e, app, build) at the same
time. The test writes `tmp/bench/start.png`, `tmp/bench/busy.png`,
`tmp/bench/result.md`, `tmp/bench/result.json` and the app's log `tmp/bench/app.log`, and lists
each target as met or MISS. The test sets `MYGO_FRAME_STATS=all` for the app, so the log has one line
a frame, and the test marks where the samples of each phase start and end.
Use `-count=1`. A miss is
reported only; it does not fail the run. The measured build has the
automation plugin (`-tags e2e`); it only listens on a socket. The app pane
is smaller than 200x60 cells, so the numbers are not the grid test load.
The test logs the pane size in points, not cells. In phase 6 the loop also
writes a counter file. herdr starts and stops it, because window input does
not reach a hidden window. If the counter is too low, the test fails (the
loop did not run, so the reading is not valid).
