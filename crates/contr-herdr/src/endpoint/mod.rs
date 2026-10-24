//! Client for herdr's endpoint socket (binary, generation 1). Written from
//! herdr's source; see NOTICE.

pub mod codec;
pub mod conn;
pub mod link;
pub mod messages;
mod osc52;
pub mod surface;
#[cfg(test)]
mod testing;
