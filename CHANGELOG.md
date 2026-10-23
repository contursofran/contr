# Changelog

This file records completed changes. Each work PR adds its entry in the same PR. Dates use YYYY-MM-DD.

## 2026-10-23

- Draw box drawing lines and block characters as quads (S8 of #4). Lines such as `│ ─ ┌ ┼`, the heavy and
  double sets, and blocks such as `█ ▀ ░` meet with no gap between rows. Rounded corners (`╭ ╮ ╯ ╰`) stay text.
