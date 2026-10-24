//! The menu bar: the app menu, Edit and Window. Each item runs an action from `actions`. Its
//! shortcut label comes from the binding that `actions::bind_keys` installs.

use gpui::{App, Menu, MenuItem, OsAction};

use crate::actions::{About, Copy, Hide, HideOthers, Minimize, Paste, Quit, SelectAll};

/// The menus in menu-bar order. macOS titles the first one with the app name.
pub fn menus() -> Vec<Menu> {
    vec![
        Menu::new("contr").items([
            MenuItem::action("About contr", About),
            MenuItem::separator(),
            MenuItem::action("Hide contr", Hide),
            MenuItem::action("Hide Others", HideOthers),
            MenuItem::separator(),
            MenuItem::action("Quit contr", Quit),
        ]),
        Menu::new("Edit").items([
            MenuItem::os_action("Copy", Copy, OsAction::Copy),
            MenuItem::os_action("Paste", Paste, OsAction::Paste),
            MenuItem::os_action("Select All", SelectAll, OsAction::SelectAll),
        ]),
        Menu::new("Window").items([MenuItem::action("Minimize", Minimize)]),
    ]
}

/// Replaces the menu bar with the menus from `menus`.
pub fn install(cx: &mut App) {
    cx.set_menus(menus());
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use gpui::{Action, TestAppContext};

    use super::*;

    /// The name of each item in `menu`, with `-` for a separator.
    fn item_names(menu: &Menu) -> Vec<String> {
        menu.items
            .iter()
            .map(|item| match item {
                MenuItem::Action { name, .. } => name.to_string(),
                MenuItem::Separator => "-".to_owned(),
                _ => "other".to_owned(),
            })
            .collect()
    }

    /// The action of the item `name` in `menu`, and its OS action.
    fn action_of<'a>(menu: &'a Menu, name: &str) -> Option<(&'a dyn Action, Option<OsAction>)> {
        menu.items.iter().find_map(|item| match item {
            MenuItem::Action {
                name: item_name,
                action,
                os_action,
                ..
            } if item_name.as_ref() == name => Some((action.as_ref(), *os_action)),
            _ => None,
        })
    }

    #[test]
    fn the_app_menu_has_about_hide_hide_others_and_quit() {
        let menus = menus();
        let app = &menus[0];
        assert_eq!(app.name.as_ref(), "contr");
        assert_eq!(
            item_names(app),
            [
                "About contr",
                "-",
                "Hide contr",
                "Hide Others",
                "-",
                "Quit contr"
            ]
        );
        assert!(action_of(app, "About contr").is_some_and(|(a, _)| a.as_any().is::<About>()));
        assert!(action_of(app, "Hide contr").is_some_and(|(a, _)| a.as_any().is::<Hide>()));
        assert!(action_of(app, "Hide Others").is_some_and(|(a, _)| a.as_any().is::<HideOthers>()));
        assert!(action_of(app, "Quit contr").is_some_and(|(a, _)| a.as_any().is::<Quit>()));
    }

    #[test]
    fn the_edit_menu_has_copy_paste_and_select_all_as_os_actions() {
        let menus = menus();
        let edit = &menus[1];
        assert_eq!(edit.name.as_ref(), "Edit");
        assert_eq!(item_names(edit), ["Copy", "Paste", "Select All"]);
        let (copy, copy_os) = action_of(edit, "Copy").unwrap();
        assert!(copy.as_any().is::<Copy>());
        assert!(matches!(copy_os, Some(OsAction::Copy)));
        let (paste, paste_os) = action_of(edit, "Paste").unwrap();
        assert!(paste.as_any().is::<Paste>());
        assert!(matches!(paste_os, Some(OsAction::Paste)));
        let (select_all, select_all_os) = action_of(edit, "Select All").unwrap();
        assert!(select_all.as_any().is::<SelectAll>());
        assert!(matches!(select_all_os, Some(OsAction::SelectAll)));
    }

    #[test]
    fn the_window_menu_has_minimize() {
        let menus = menus();
        let window = &menus[2];
        assert_eq!(window.name.as_ref(), "Window");
        assert_eq!(item_names(window), ["Minimize"]);
        assert!(action_of(window, "Minimize").is_some_and(|(a, _)| a.as_any().is::<Minimize>()));
    }

    #[gpui::test]
    fn install_puts_the_menus_in_the_menu_bar(cx: &mut TestAppContext) {
        cx.update(install);

        let names: Vec<String> = cx
            .update(|app| app.get_menus())
            .unwrap_or_default()
            .iter()
            .map(|menu| menu.name.to_string())
            .collect();
        assert_eq!(names, ["contr", "Edit", "Window"]);
    }
}
