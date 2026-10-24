//! contr: a native herdr client.

pub mod agents_list;
pub mod board;
pub mod board_actions;
pub mod board_card;
pub mod board_model;
pub mod board_needs;
pub mod board_outbox;
pub mod board_panel;
pub mod board_report;
pub mod board_work;
pub mod dev_client;
#[cfg(debug_assertions)]
pub mod dev_control;
pub mod dev_protocol;
pub mod frame;
pub mod git;
pub mod herdr;
pub mod host;
pub mod legend;
pub mod report;
pub mod selection;
pub mod spaces;
pub mod spaces_model;
pub mod status_bar;
pub mod status_icon;
pub mod status_message;
pub mod tab_commands;
pub mod terminal;
pub mod theme;
pub mod title_bar;
pub mod usage;
pub mod visibility;
