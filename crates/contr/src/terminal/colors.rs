//! Program colours for the terminal. The terminal shows the colours a program asks for, as herdr sends
//! them (D10); greyscale applies to contr's own UI only. A packed colour is herdr's `u32`: the top byte
//! is the kind (0 named, 1 indexed, 2 RGB) and the low bits are the value.

use contr_herdr::endpoint::surface::Cell;

use crate::theme::{BG_TERMINAL, TEXT};

/// The 16 ANSI colours in herdr's order: black, red, green, yellow, blue, magenta, cyan, white, then
/// the bright eight. Own values, readable on `BG_TERMINAL`.
pub const PALETTE_16: [u32; 16] = [
    0x1E1E1E, 0xE06C6C, 0x8CC07A, 0xE0C070, 0x6CA8E8, 0xC080D8, 0x5CC0C8, 0xD0D0D0, 0x5C5C5C,
    0xFF8080, 0xA8E090, 0xFFD890, 0x90C4FF, 0xDCA0F0, 0x80DCE4, 0xFFFFFF,
];

const KIND_NAMED: u32 = 0;
const KIND_INDEXED: u32 = 1;
const KIND_RGB: u32 = 2;

/// herdr's modifier bits that change colours (`Cell::attrs`).
const DIM: u16 = 2;
const REVERSED: u16 = 64;
const HIDDEN: u16 = 128;

/// The colour (0xRRGGBB) of a packed herdr colour, or `default` when it names the default colour.
pub fn resolve(v: u32, default: u32) -> u32 {
    resolve_explicit(v).unwrap_or(default)
}

/// `Some` for a colour the program set; `None` for the default colour and for kinds this client does
/// not know.
fn resolve_explicit(v: u32) -> Option<u32> {
    match v >> 24 {
        KIND_NAMED => match v {
            // Named 1..=16 are ANSI 0..=15; named 0 is the default colour.
            1..=16 => PALETTE_16.get((v - 1) as usize).copied(),
            _ => None,
        },
        KIND_INDEXED => indexed((v & 0xFF) as u8),
        KIND_RGB => Some(v & 0xFF_FFFF),
        _ => None,
    }
}

/// xterm's 256 colours: 0–15 are the ANSI palette, 16–231 a 6×6×6 cube, 232–255 a grey ramp.
fn indexed(i: u8) -> Option<u32> {
    match i {
        0..=15 => PALETTE_16.get(usize::from(i)).copied(),
        16..=231 => {
            let n = u32::from(i - 16);
            let (r, g, b) = (
                cube_level(n / 36),
                cube_level((n / 6) % 6),
                cube_level(n % 6),
            );
            Some((r << 16) | (g << 8) | b)
        }
        232..=255 => {
            let grey = 8 + 10 * u32::from(i - 232);
            Some((grey << 16) | (grey << 8) | grey)
        }
    }
}

/// One level of the cube: 0, then 95, 135, 175, 215 and 255.
fn cube_level(n: u32) -> u32 {
    if n == 0 { 0 } else { 55 + 40 * n }
}

/// Halves `fg` toward `bg`, channel by channel.
fn dim(fg: u32, bg: u32) -> u32 {
    let channel = |shift: u32| (((fg >> shift) & 0xFF) + ((bg >> shift) & 0xFF)) / 2;
    (channel(16) << 16) | (channel(8) << 8) | channel(0)
}

fn has(cell: &Cell, flag: u16) -> bool {
    (cell.attrs & flag) != 0
}

/// The foreground (0xRRGGBB) and background to paint a cell with. `None` as the background is the
/// default background, which the view paints as `BG_TERMINAL`. REVERSED swaps the two colours, DIM
/// halves the foreground toward the background, and HIDDEN paints the foreground in the background
/// colour, so the glyph does not show.
pub fn cell_colors(cell: &Cell) -> (u32, Option<u32>) {
    let fg = resolve_explicit(cell.fg);
    let bg = resolve_explicit(cell.bg);
    let (fg, bg) = if has(cell, REVERSED) {
        (bg.unwrap_or(BG_TERMINAL), Some(fg.unwrap_or(TEXT)))
    } else {
        (fg.unwrap_or(TEXT), bg)
    };
    let backdrop = bg.unwrap_or(BG_TERMINAL);
    let fg = if has(cell, DIM) {
        dim(fg, backdrop)
    } else {
        fg
    };
    let fg = if has(cell, HIDDEN) { backdrop } else { fg };
    (fg, bg)
}

#[cfg(test)]
mod tests {
    use super::*;

    const DEFAULT: u32 = 0;

    fn cell(fg: u32, bg: u32, attrs: u16) -> Cell {
        Cell {
            text: "a".into(),
            fg,
            bg,
            attrs,
            wide_tail: false,
        }
    }

    #[test]
    fn named_zero_is_the_default_colour() {
        assert_eq!(resolve(DEFAULT, TEXT), TEXT);
        assert_eq!(resolve(DEFAULT, BG_TERMINAL), BG_TERMINAL);
        assert_eq!(cell_colors(&cell(DEFAULT, DEFAULT, 0)), (TEXT, None));
    }

    #[test]
    fn named_colours_map_to_the_ansi_palette() {
        assert_eq!(resolve(1, TEXT), PALETTE_16[0]); // black
        assert_eq!(resolve(2, TEXT), PALETTE_16[1]); // red
        assert_eq!(resolve(16, TEXT), PALETTE_16[15]); // bright white
    }

    #[test]
    fn named_values_past_the_palette_are_the_default() {
        assert_eq!(resolve(17, TEXT), TEXT);
        assert_eq!(resolve(0xFF, BG_TERMINAL), BG_TERMINAL);
    }

    #[test]
    fn indexed_0_to_15_are_the_palette() {
        assert_eq!(resolve(0x0100_0001, TEXT), PALETTE_16[1]);
        assert_eq!(resolve(0x0100_000F, TEXT), PALETTE_16[15]);
    }

    #[test]
    fn indexed_cube_and_grey_ramp_follow_xterm() {
        let idx = |i: u32| resolve(0x0100_0000 | i, TEXT);
        assert_eq!(idx(16), 0x000000);
        assert_eq!(idx(21), 0x0000FF);
        assert_eq!(idx(196), 0xFF0000);
        assert_eq!(idx(231), 0xFFFFFF);
        assert_eq!(idx(232), 0x080808);
        assert_eq!(idx(255), 0xEEEEEE);
    }

    #[test]
    fn rgb_colours_pass_through() {
        assert_eq!(resolve(0x0212_3456, TEXT), 0x12_3456);
    }

    #[test]
    fn unknown_kinds_are_the_default_colour() {
        assert_eq!(resolve(0x0312_3456, TEXT), TEXT);
        assert_eq!(
            cell_colors(&cell(0x0300_0000, 0x0300_0000, 0)),
            (TEXT, None)
        );
    }

    #[test]
    fn explicit_colours_are_kept_and_the_default_background_stays_none() {
        assert_eq!(
            cell_colors(&cell(DEFAULT, 0x02FF_0000, 0)),
            (TEXT, Some(0xFF0000))
        );
    }

    #[test]
    fn reversed_with_default_colours_paints_the_background_as_text() {
        assert_eq!(
            cell_colors(&cell(DEFAULT, DEFAULT, REVERSED)),
            (BG_TERMINAL, Some(TEXT))
        );
    }

    #[test]
    fn reversed_swaps_explicit_colours() {
        assert_eq!(
            cell_colors(&cell(0x02FF_0000, 0x0200_FF00, REVERSED)),
            (0x00FF00, Some(0xFF0000))
        );
    }

    #[test]
    fn dim_halves_the_foreground_toward_the_background() {
        // (0xED + 0x12) / 2 = 0x7F on each channel; the background is the default.
        assert_eq!(cell_colors(&cell(DEFAULT, DEFAULT, DIM)), (0x7F7F7F, None));
    }

    #[test]
    fn hidden_paints_the_text_in_the_background_colour() {
        assert_eq!(
            cell_colors(&cell(0x02FF_0000, DEFAULT, HIDDEN)),
            (BG_TERMINAL, None)
        );
        assert_eq!(
            cell_colors(&cell(0x02FF_0000, 0x0200_FF00, HIDDEN)),
            (0x00FF00, Some(0x00FF00))
        );
    }
}
