//! The needs-you section of the board panel: the `Needs you N` head with the `clear done` control,
//! then one row per item in the coordinator's order. The rows are plain elements. Their icons
//! are static, so the section needs no cached views. Read-only in #9: rows never move on their own.

use crate::board_card::selected_card;
use crate::board_model::{ItemKind, NeedItem, item_kind_label, item_status};
use crate::board_panel::{ActionClick, clear_done};
use crate::status_icon::StatusIcon;
use crate::theme::*;
use gpui::{App, ClickEvent, FontWeight, Role, Window, div, prelude::*, px, rgb};
use std::rc::Rc;

/// Picks the needs-you item with the given key. A row click calls it with the row's key.
pub type PickFn = Rc<dyn Fn(String, &mut App)>;

/// The needs-you section. It takes the rest of the panel's height, and its rows scroll inside it.
/// `picked` is the key of the picked item, if the pick is an item. A click on a row calls `on_pick`.
/// `on_clear` is the `clear done` control of the head, or `None` when the board takes no actions.
pub fn needs_section(
    items: &[NeedItem],
    need_count: u32,
    clear_items: bool,
    picked: Option<&str>,
    on_pick: PickFn,
    on_clear: Option<ActionClick>,
) -> impl IntoElement {
    let rows = div()
        .id("needs-rows")
        .role(Role::List)
        .debug_selector(|| "needs-rows".into())
        .flex_1()
        .min_h_0()
        .flex()
        .flex_col()
        .overflow_y_scroll();
    let rows = if items.is_empty() {
        rows.child(empty_line())
    } else {
        rows.children(items.iter().enumerate().map(|(index, item)| {
            let is_picked = picked == Some(item.key.as_str());
            need_row(index, item, is_picked, on_pick.clone())
        }))
    };
    div()
        .debug_selector(|| "needs-section".into())
        .flex_1()
        .min_h_0()
        .flex()
        .flex_col()
        .child(head(need_count, clear_items, on_clear))
        .child(rows)
}

/// `Needs you`, the open count in bold, then `clear done` while the section has resolved items.
fn head(need_count: u32, clear_items: bool, on_clear: Option<ActionClick>) -> impl IntoElement {
    div()
        .debug_selector(|| "needs-header".into())
        .h(px(BOARD_LINE_HEIGHT))
        .flex_none()
        .flex()
        .items_center()
        .gap(px(NEED_GAP))
        .px(px(PANEL_PAD_X))
        .child(div().text_color(rgb(TEXT_DIM)).child("Needs you"))
        .child(
            div()
                .debug_selector(move || format!("needs-count-{need_count}"))
                .font_weight(FontWeight::BOLD)
                .text_color(rgb(TEXT))
                .child(need_count.to_string()),
        )
        .when(clear_items, |line| {
            line.child(clear_done("needs-clear", on_clear))
        })
}

/// The line in place of the rows when no item waits.
fn empty_line() -> impl IntoElement {
    div()
        .debug_selector(|| "needs-empty".into())
        .h(px(BOARD_LINE_HEIGHT))
        .flex_none()
        .flex()
        .items_center()
        .px(px(PANEL_PAD_X))
        .text_color(rgb(TEXT_DIM))
        .child("Nothing waits for you.")
}

/// One item: the status icon, the key, the title and the kind word on the right. A resolved item
/// has its title struck through. A picked item draws as a selected card, and a click picks it.
fn need_row(index: usize, item: &NeedItem, picked: bool, on_pick: PickFn) -> impl IntoElement {
    let text = need_text(item);
    let key = item.key.clone();
    let title = div()
        .min_w_0()
        .flex_1()
        .overflow_hidden()
        .text_ellipsis()
        .whitespace_nowrap()
        .text_color(rgb(text))
        .child(item.title.clone())
        .when(!item.open, |title| {
            title
                .line_through()
                .debug_selector(move || format!("need-row-{index}-resolved"))
        });
    div()
        .id(("need-row", index))
        .debug_selector(move || format!("need-row-{index}"))
        .role(Role::ListItem)
        .aria_label(need_label(item))
        .h(px(BOARD_LINE_HEIGHT))
        .flex_none()
        .flex()
        .items_center()
        .gap(px(NEED_GAP))
        .px(px(PANEL_PAD_X))
        .on_click(move |_: &ClickEvent, _: &mut Window, cx: &mut App| {
            on_pick(key.clone(), cx);
        })
        // The selected card is the first child, so it is drawn behind the row's content.
        .when(picked, |row| {
            row.child(selected_card(format!("need-row-{index}-picked")))
        })
        .child(StatusIcon::new(("need-icon", index), item_status(item)).size(px(ICON_SIZE_SMALL)))
        .child(
            div()
                .flex_none()
                .w(px(NEED_KEY_WIDTH))
                .text_color(rgb(text))
                .child(item.key.clone()),
        )
        .child(title)
        .child(
            div()
                .flex_none()
                .text_color(rgb(TEXT_DIM))
                .child(item_kind_label(item)),
        )
}

/// Open asks and to-dos wait on the owner, so their key and title read in `TEXT`. Resolved items
/// and news read in `TEXT_DIM`.
fn need_text(item: &NeedItem) -> u32 {
    match (item.open, item.kind) {
        (true, ItemKind::Ask | ItemKind::Do) => TEXT,
        _ => TEXT_DIM,
    }
}

/// The accessible name of a row: `{key} {title}, {kind label}`.
fn need_label(item: &NeedItem) -> String {
    format!("{} {}, {}", item.key, item.title, item_kind_label(item))
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use crate::board_model::parse_board;
    use gpui::{
        Bounds, Context, Modifiers, Pixels, Render, TestAppContext, VisualTestContext, Window, px,
        size,
    };
    use std::cell::RefCell;

    const FIXTURE: &str = include_str!("board_fixture_v1.json");

    fn fixture_items() -> Vec<NeedItem> {
        parse_board(FIXTURE.as_bytes()).unwrap().items
    }

    fn item(json: &str) -> NeedItem {
        serde_json::from_str(json).unwrap()
    }

    /// A pick callback that does nothing.
    fn ignore() -> PickFn {
        Rc::new(|_, _| {})
    }

    /// The section in a panel-sized column, as `BoardPanel` draws it.
    struct Host {
        items: Vec<NeedItem>,
        need_count: u32,
        clear_items: bool,
        picked: Option<String>,
        on_pick: PickFn,
    }

    impl Render for Host {
        fn render(&mut self, _window: &mut Window, _cx: &mut Context<Self>) -> impl IntoElement {
            div()
                .debug_selector(|| "host".into())
                .size_full()
                .flex()
                .flex_col()
                .child(needs_section(
                    &self.items,
                    self.need_count,
                    self.clear_items,
                    self.picked.as_deref(),
                    self.on_pick.clone(),
                    None,
                ))
        }
    }

    /// A window of the panel's size with the section in it, with nothing picked.
    fn host(
        cx: &mut TestAppContext,
        items: Vec<NeedItem>,
        need_count: u32,
        clear_items: bool,
    ) -> &mut VisualTestContext {
        host_with(cx, items, need_count, clear_items, None, ignore())
    }

    /// `host`, with the picked key and the pick callback given.
    fn host_with(
        cx: &mut TestAppContext,
        items: Vec<NeedItem>,
        need_count: u32,
        clear_items: bool,
        picked: Option<String>,
        on_pick: PickFn,
    ) -> &mut VisualTestContext {
        let (_view, cx) = cx.add_window_view(move |_, _| Host {
            items,
            need_count,
            clear_items,
            picked,
            on_pick,
        });
        cx.simulate_resize(size(px(440.), px(900.)));
        cx.run_until_parked();
        cx
    }

    fn bounds(cx: &mut VisualTestContext, selector: &'static str) -> Option<Bounds<Pixels>> {
        cx.debug_bounds(selector)
    }

    /// Clicks the centre of the element with the given selector, as the mouse would.
    fn click(cx: &mut VisualTestContext, selector: &'static str) {
        let bounds = cx.debug_bounds(selector).unwrap();
        cx.simulate_click(bounds.center(), Modifiers::none());
        cx.run_until_parked();
    }

    /// Whether `inner` lies inside `outer`, top and bottom.
    fn inside(outer: Bounds<Pixels>, inner: Bounds<Pixels>) -> bool {
        inner.origin.y >= outer.origin.y && inner.bottom() <= outer.bottom()
    }

    #[test]
    fn open_asks_and_to_dos_read_in_the_text_colour() {
        assert_eq!(need_text(&item(r#"{"key":"D7","kind":"ask"}"#)), TEXT);
        assert_eq!(need_text(&item(r#"{"key":"T12","kind":"do"}"#)), TEXT);
        assert_eq!(need_text(&item(r#"{"key":"N4","kind":"know"}"#)), TEXT_DIM);
        assert_eq!(need_text(&item(r#"{"key":"N5","kind":"news"}"#)), TEXT_DIM);
        assert_eq!(
            need_text(&item(r#"{"key":"D6","kind":"ask","open":false}"#)),
            TEXT_DIM
        );
    }

    #[test]
    fn the_accessible_name_is_key_title_and_kind() {
        assert_eq!(
            need_label(&item(r#"{"key":"D7","kind":"ask","title":"Row layout"}"#)),
            "D7 Row layout, decide"
        );
        assert_eq!(
            need_label(&item(
                r#"{"key":"D6","kind":"ask","title":"Colours","open":false}"#
            )),
            "D6 Colours, done"
        );
    }

    #[gpui::test]
    fn the_fixture_rows_are_28_tall_and_hold_their_icons(cx: &mut TestAppContext) {
        let cx = host(cx, fixture_items(), 3, true);
        assert!(bounds(cx, "needs-count-3").is_some());
        let rows = ["need-row-0", "need-row-1", "need-row-2", "need-row-3"];
        for row in rows {
            let row = bounds(cx, row).unwrap();
            assert_eq!(row.size.height, px(BOARD_LINE_HEIGHT));
        }
        let icons = [
            ("need-row-0", "icon-do"),
            ("need-row-1", "icon-decide"),
            ("need-row-2", "icon-fyi"),
            ("need-row-3", "icon-done"),
        ];
        for (row, icon) in icons {
            let row = bounds(cx, row).unwrap();
            let icon = bounds(cx, icon).unwrap();
            assert!(inside(row, icon), "{icon:?} is not in {row:?}");
        }
    }

    #[gpui::test]
    fn only_the_resolved_row_has_the_resolved_marker(cx: &mut TestAppContext) {
        let cx = host(cx, fixture_items(), 3, true);
        assert!(bounds(cx, "need-row-3-resolved").is_some());
        for selector in [
            "need-row-0-resolved",
            "need-row-1-resolved",
            "need-row-2-resolved",
        ] {
            assert!(bounds(cx, selector).is_none(), "{selector} is set");
        }
    }

    #[gpui::test]
    fn clear_done_shows_with_clear_items(cx: &mut TestAppContext) {
        let shown = host(cx, fixture_items(), 3, true);
        assert!(bounds(shown, "needs-clear").is_some());
    }

    #[gpui::test]
    fn clear_done_is_hidden_without_resolved_items(cx: &mut TestAppContext) {
        let hidden = host(cx, fixture_items(), 3, false);
        assert!(bounds(hidden, "needs-clear").is_none());
    }

    #[gpui::test]
    fn items_keep_the_coordinators_order(cx: &mut TestAppContext) {
        let items = vec![
            item(r#"{"key":"T3","kind":"do","title":"Test"}"#),
            item(r#"{"key":"D1","kind":"ask","title":"Pick"}"#),
        ];
        let cx = host(cx, items, 2, false);
        let first = bounds(cx, "need-row-0").unwrap();
        let second = bounds(cx, "need-row-1").unwrap();
        assert!(first.origin.y < second.origin.y, "row 0 is above row 1");
        assert!(inside(first, bounds(cx, "icon-do").unwrap()), "T3 is row 0");
        assert!(
            inside(second, bounds(cx, "icon-decide").unwrap()),
            "D1 is row 1"
        );
    }

    #[gpui::test]
    fn no_items_shows_the_empty_line(cx: &mut TestAppContext) {
        let cx = host(cx, Vec::new(), 0, false);
        assert!(bounds(cx, "needs-empty").is_some());
        assert!(bounds(cx, "need-row-0").is_none());
    }

    #[gpui::test]
    fn the_picked_item_draws_as_the_selected_card(cx: &mut TestAppContext) {
        let cx = host_with(
            cx,
            fixture_items(),
            3,
            true,
            Some("D7".to_owned()),
            ignore(),
        );
        assert!(bounds(cx, "need-row-1-picked").is_some(), "D7 is picked");
        assert!(bounds(cx, "need-row-0").is_some(), "T12 is not picked");
        assert!(bounds(cx, "need-row-0-picked").is_none());
    }

    #[gpui::test]
    fn a_click_on_a_row_picks_its_key(cx: &mut TestAppContext) {
        let picks = Rc::new(RefCell::new(Vec::new()));
        let log = picks.clone();
        let on_pick: PickFn = Rc::new(move |key, _| log.borrow_mut().push(key));
        let cx = host_with(cx, fixture_items(), 3, true, None, on_pick);
        click(cx, "need-row-2");
        assert_eq!(*picks.borrow(), vec!["N4".to_owned()]);
    }

    #[gpui::test]
    fn the_section_ends_at_the_bottom_and_the_rows_clip(cx: &mut TestAppContext) {
        let many: Vec<NeedItem> = (0..40)
            .map(|i| {
                item(&format!(
                    r#"{{"key":"D{i}","kind":"ask","title":"Item {i}"}}"#
                ))
            })
            .collect();
        let cx = host(cx, many, 40, false);
        let host = bounds(cx, "host").unwrap();
        let section = bounds(cx, "needs-section").unwrap();
        let rows = bounds(cx, "needs-rows").unwrap();
        assert_eq!(
            section.bottom(),
            host.bottom(),
            "the section ends at the bottom"
        );
        assert_eq!(rows.bottom(), host.bottom(), "the rows fill the rest");
        let last = bounds(cx, "need-row-39").unwrap();
        assert!(
            last.origin.y > host.bottom(),
            "the last row is laid out below the panel and clipped by the rows"
        );
    }
}
