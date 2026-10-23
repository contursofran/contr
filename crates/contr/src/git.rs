//! Git checkouts for the sidebar: which checkout holds a directory, and which
//! worktrees a repository has. Reads plain files only (`.git` entries,
//! `commondir`, `gitdir`, `worktrees/<name>/`, `HEAD`). It never runs `git`.

use std::collections::{HashMap, HashSet};
use std::ffi::OsStr;
use std::fs;
use std::io;
use std::path::{Path, PathBuf};

/// One checkout (the main one or a linked worktree) that holds a directory.
/// All paths are canonical.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Checkout {
    /// Working tree root.
    pub root: PathBuf,
    /// `<main>/.git`, or `<main>/.git/worktrees/<name>` for a linked worktree.
    pub git_dir: PathBuf,
    /// Shared git dir of the repository: `<main>/.git`.
    pub common_dir: PathBuf,
    /// True for the main checkout, whose `.git` is a directory.
    pub is_main: bool,
}

/// One worktree of a repository. `name` is the root's directory name.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WorktreeEntry {
    pub root: PathBuf,
    pub name: String,
    /// Branch name; a detached HEAD gives its first 7 hex digits.
    pub branch: Option<String>,
    pub is_main: bool,
}

/// A repository: its main checkout and its worktrees.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RepoInfo {
    pub common_dir: PathBuf,
    /// Directory name of the main root.
    pub name: String,
    pub main_root: PathBuf,
    pub main_branch: Option<String>,
    /// Main first, then linked worktrees sorted by root path.
    pub worktrees: Vec<WorktreeEntry>,
}

/// Checkouts and repositories for a set of pane working directories.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Scan {
    /// Keyed by the cwd string as herdr sent it.
    pub checkouts: HashMap<String, Checkout>,
    /// Keyed by `common_dir`. Every checkout's `common_dir` is a key here.
    pub repos: HashMap<PathBuf, RepoInfo>,
}

/// The checkout that holds `path`: the nearest ancestor (or `path` itself) with
/// a `.git` entry. `None` when `path` does not exist, lies outside git, or
/// reaches a bare repository. A `.git` file without `commondir` whose git dir
/// is not `<main>/.git` (a submodule or a separate git dir) also gives `None`,
/// so such panes are not in Spaces.
pub fn discover(path: &Path) -> Option<Checkout> {
    let start = fs::canonicalize(path).ok()?;
    for root in start.ancestors() {
        let dot_git = root.join(".git");
        if dot_git.is_dir() {
            let git_dir = fs::canonicalize(dot_git).ok()?;
            return Some(Checkout {
                root: root.to_path_buf(),
                common_dir: git_dir.clone(),
                git_dir,
                is_main: true,
            });
        }
        if dot_git.is_file() {
            return linked_checkout(root, &dot_git);
        }
    }
    None
}

/// Branch of the checkout whose git dir is `git_dir`, read from `HEAD`:
/// `ref: refs/heads/<name>` gives `<name>`, a detached commit gives its first 7
/// hex digits. `None` when `HEAD` is unreadable or names neither.
pub fn head_branch(git_dir: &Path) -> Option<String> {
    let text = fs::read_to_string(git_dir.join("HEAD")).ok()?;
    let head = text.trim();
    if let Some(reference) = head.strip_prefix("ref:") {
        let branch = reference.trim().strip_prefix("refs/heads/")?;
        return (!branch.is_empty()).then(|| branch.to_owned());
    }
    let short = head.get(..7)?;
    head.bytes()
        .all(|b| b.is_ascii_hexdigit())
        .then(|| short.to_owned())
}

/// The main checkout and the linked worktrees of the repository whose shared git
/// dir is `common_dir`. Worktrees whose `.git` file is gone are left out.
/// `Err` when the repository cannot be read, including a bare repository.
pub fn read_repo(common_dir: &Path) -> io::Result<RepoInfo> {
    let common_dir = fs::canonicalize(common_dir)?;
    let main_root = main_root_of(&common_dir)
        .ok_or_else(|| invalid_input("not a `.git` directory"))?
        .to_path_buf();
    let name = dir_name(&main_root).ok_or_else(|| invalid_input("main root has no name"))?;
    let main_branch = head_branch(&common_dir);
    let mut worktrees = vec![WorktreeEntry {
        root: main_root.clone(),
        name: name.clone(),
        branch: main_branch.clone(),
        is_main: true,
    }];
    let mut linked = linked_worktrees(&common_dir)?;
    linked.sort_by(|a, b| a.root.cmp(&b.root));
    worktrees.append(&mut linked);
    Ok(RepoInfo {
        common_dir,
        name,
        main_root,
        main_branch,
        worktrees,
    })
}

/// Checkouts and repositories for `cwds`. Blocking: call it only from a
/// background executor. A cwd outside git is left out. A repository that
/// cannot be read is left out too, with every checkout of it.
pub fn scan(cwds: &[String]) -> Scan {
    let mut scan = Scan::default();
    let mut seen = HashSet::new();
    for cwd in cwds {
        if !seen.insert(cwd.as_str()) {
            continue;
        }
        if let Some(checkout) = discover(Path::new(cwd)) {
            scan.checkouts.insert(cwd.clone(), checkout);
        }
    }
    let common_dirs: HashSet<PathBuf> = scan
        .checkouts
        .values()
        .map(|checkout| checkout.common_dir.clone())
        .collect();
    for common_dir in common_dirs {
        if let Ok(repo) = read_repo(&common_dir) {
            scan.repos.insert(repo.common_dir.clone(), repo);
        }
    }
    let repos = &scan.repos;
    scan.checkouts
        .retain(|_, checkout| repos.contains_key(&checkout.common_dir));
    scan
}

/// A linked worktree, from its `.git` file (`gitdir: <admin dir>`).
fn linked_checkout(root: &Path, dot_git: &Path) -> Option<Checkout> {
    let text = fs::read_to_string(dot_git).ok()?;
    let target = text.trim().strip_prefix("gitdir:")?.trim();
    if target.is_empty() {
        return None;
    }
    let git_dir = fs::canonicalize(root.join(target)).ok()?;
    let common_dir = common_dir_of(&git_dir)?;
    main_root_of(&common_dir)?;
    Some(Checkout {
        root: root.to_path_buf(),
        git_dir,
        common_dir,
        is_main: false,
    })
}

/// The shared git dir named by `commondir` (relative to `git_dir`). Without the
/// file, `git_dir` is its own common dir.
fn common_dir_of(git_dir: &Path) -> Option<PathBuf> {
    match fs::read_to_string(git_dir.join("commondir")) {
        Ok(text) => fs::canonicalize(git_dir.join(text.trim())).ok(),
        Err(err) if err.kind() == io::ErrorKind::NotFound => Some(git_dir.to_path_buf()),
        Err(_) => None,
    }
}

/// The main root of a shared git dir: `<root>/.git` gives `<root>`. `None` for
/// bare repositories and any other layout.
fn main_root_of(common_dir: &Path) -> Option<&Path> {
    if common_dir.file_name() != Some(OsStr::new(".git")) {
        return None;
    }
    common_dir.parent()
}

/// Linked worktrees listed under `<common>/worktrees/`, unsorted. Missing
/// directory: none.
fn linked_worktrees(common_dir: &Path) -> io::Result<Vec<WorktreeEntry>> {
    let entries = match fs::read_dir(common_dir.join("worktrees")) {
        Ok(entries) => entries,
        Err(err) if err.kind() == io::ErrorKind::NotFound => return Ok(Vec::new()),
        Err(err) => return Err(err),
    };
    let mut linked = Vec::new();
    for entry in entries {
        let admin = entry?.path();
        if !admin.is_dir() {
            continue;
        }
        let Some(root) = linked_root(&admin) else {
            continue;
        };
        let Some(name) = dir_name(&root) else {
            continue;
        };
        linked.push(WorktreeEntry {
            root,
            name,
            branch: head_branch(&admin),
            is_main: false,
        });
    }
    Ok(linked)
}

/// Root of the worktree an admin dir points at: its `gitdir` file names
/// `<root>/.git`, relative to the admin dir or absolute. `None` when the file
/// is missing or empty, or the `.git` file is gone.
fn linked_root(admin: &Path) -> Option<PathBuf> {
    let text = fs::read_to_string(admin.join("gitdir")).ok()?;
    let target = text.trim();
    if target.is_empty() {
        return None;
    }
    let dot_git = fs::canonicalize(admin.join(target)).ok()?;
    dot_git.parent().map(Path::to_path_buf)
}

fn dir_name(path: &Path) -> Option<String> {
    path.file_name()
        .map(|name| name.to_string_lossy().into_owned())
}

fn invalid_input(message: &str) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidInput, message)
}

/// Fake git layouts on disk for tests (plain files, no `git` binary). Shared with
/// the sidebar tests.
#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
pub(crate) mod fake {
    use std::fs;
    use std::path::{Path, PathBuf};

    /// Writes `text` to `path`, creating parent directories.
    pub(crate) fn write(path: &Path, text: &str) {
        fs::create_dir_all(path.parent().unwrap()).unwrap();
        fs::write(path, text).unwrap();
    }

    /// Main checkout at `root` with `.git/HEAD` = `head`. Returns the git dir.
    pub(crate) fn main_repo(root: &Path, head: &str) -> PathBuf {
        let git_dir = root.join(".git");
        write(&git_dir.join("HEAD"), head);
        git_dir
    }

    /// Linked worktree at `root` with admin dir `<common>/worktrees/<name>`, laid
    /// out as git writes it: absolute `gitdir`, `commondir` `../..`, and a
    /// `.git` file pointing at the admin dir. Returns the admin dir.
    pub(crate) fn linked(common: &Path, name: &str, root: &Path, head: &str) -> PathBuf {
        let admin = common.join("worktrees").join(name);
        write(&admin.join("HEAD"), head);
        write(
            &admin.join("gitdir"),
            &format!("{}\n", root.join(".git").display()),
        );
        write(&admin.join("commondir"), "../..\n");
        write(
            &root.join(".git"),
            &format!("gitdir: {}\n", admin.display()),
        );
        admin
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use tempfile::TempDir;

    use super::fake::{linked, main_repo, write};
    use super::*;

    const SHA: &str = "0123456789abcdef0123456789abcdef01234567";

    /// Temp dir and its canonical path.
    fn tmp() -> (TempDir, PathBuf) {
        let dir = tempfile::tempdir().unwrap();
        let path = fs::canonicalize(dir.path()).unwrap();
        (dir, path)
    }

    #[test]
    fn head_branch_reads_branches_and_short_shas() {
        let (_guard, base) = tmp();
        let git = base.join(".git");
        write(&git.join("HEAD"), "ref: refs/heads/agent/6-x\n");
        assert_eq!(head_branch(&git), Some("agent/6-x".to_owned()));
        write(&git.join("HEAD"), &format!("{SHA}\n"));
        assert_eq!(head_branch(&git), Some("0123456".to_owned()));
    }

    #[test]
    fn head_branch_rejects_heads_that_name_nothing() {
        let (_guard, base) = tmp();
        let git = base.join(".git");
        assert_eq!(head_branch(&git), None, "no HEAD file");
        for head in ["ref: refs/heads/", "ref: refs/remotes/o/main", "xyz", ""] {
            write(&git.join("HEAD"), head);
            assert_eq!(head_branch(&git), None, "HEAD {head:?}");
        }
    }

    #[test]
    fn discover_finds_the_main_checkout_from_a_nested_dir() {
        let (_guard, base) = tmp();
        let repo = base.join("repo");
        let git = main_repo(&repo, "ref: refs/heads/main\n");
        let deep = repo.join("src/a/b");
        fs::create_dir_all(&deep).unwrap();
        let found = discover(&deep).unwrap();
        assert_eq!(
            found,
            Checkout {
                root: repo,
                git_dir: git.clone(),
                common_dir: git.clone(),
                is_main: true,
            }
        );
        assert_eq!(head_branch(&found.git_dir), Some("main".to_owned()));
    }

    #[test]
    fn discover_outside_git_or_missing_is_none() {
        let (_guard, base) = tmp();
        fs::create_dir_all(base.join("plain")).unwrap();
        assert_eq!(discover(&base.join("plain")), None);
        assert_eq!(discover(&base.join("missing")), None);
    }

    #[test]
    fn discover_resolves_a_linked_worktree_deep_inside_it() {
        let (_guard, base) = tmp();
        let repo = base.join("repo");
        let git = main_repo(&repo, "ref: refs/heads/main\n");
        let wt = base.join("wt1");
        let admin = linked(&git, "w1", &wt, "ref: refs/heads/feature/x\n");
        fs::create_dir_all(wt.join("src/deep")).unwrap();
        let found = discover(&wt.join("src/deep")).unwrap();
        assert_eq!(
            found,
            Checkout {
                root: wt,
                git_dir: admin,
                common_dir: git,
                is_main: false,
            }
        );
    }

    #[test]
    fn relative_gitdir_and_commondir_resolve() {
        let (_guard, base) = tmp();
        let repo = base.join("repo");
        let git = main_repo(&repo, "ref: refs/heads/main\n");
        let wt = base.join("wt2");
        let admin = git.join("worktrees/w2");
        write(&admin.join("HEAD"), &format!("{SHA}\n"));
        write(&admin.join("gitdir"), "../../../../wt2/.git\n");
        write(&admin.join("commondir"), "../..\n");
        write(&wt.join(".git"), "gitdir: ../repo/.git/worktrees/w2\n");
        let found = discover(&wt).unwrap();
        assert_eq!(found.root, wt);
        assert_eq!(found.common_dir, git);
        assert!(!found.is_main);
        let repo_info = read_repo(&git).unwrap();
        assert_eq!(repo_info.worktrees[1].root, wt);
        assert_eq!(repo_info.worktrees[1].branch, Some("0123456".to_owned()));
    }

    #[test]
    fn nested_worktree_inside_the_main_root_is_linked_not_main() {
        let (_guard, base) = tmp();
        let repo = base.join("repo");
        let git = main_repo(&repo, "ref: refs/heads/main\n");
        let wt = repo.join(".worktrees/w1");
        linked(&git, "w1", &wt, "ref: refs/heads/b\n");
        fs::create_dir_all(wt.join("src")).unwrap();
        fs::create_dir_all(repo.join("src")).unwrap();
        let inside = discover(&wt.join("src")).unwrap();
        assert!(!inside.is_main);
        assert_eq!(inside.root, wt);
        assert!(discover(&repo.join("src")).unwrap().is_main);
    }

    #[test]
    fn read_repo_lists_main_first_then_linked_by_path() {
        let (_guard, base) = tmp();
        let repo = base.join("repo");
        let git = main_repo(&repo, "ref: refs/heads/main\n");
        linked(&git, "one", &base.join("zz-wt"), "ref: refs/heads/z\n");
        linked(&git, "two", &base.join("aa-wt"), &format!("{SHA}\n"));
        let info = read_repo(&git).unwrap();
        assert_eq!(info.common_dir, git);
        assert_eq!(info.name, "repo");
        assert_eq!(info.main_root, repo);
        assert_eq!(info.main_branch, Some("main".to_owned()));
        let rows: Vec<(&str, bool, Option<&str>)> = info
            .worktrees
            .iter()
            .map(|w| (w.name.as_str(), w.is_main, w.branch.as_deref()))
            .collect();
        assert_eq!(
            rows,
            vec![
                ("repo", true, Some("main")),
                ("aa-wt", false, Some("0123456")),
                ("zz-wt", false, Some("z")),
            ]
        );
    }

    #[test]
    fn read_repo_without_worktrees_dir_lists_only_main() {
        let (_guard, base) = tmp();
        let git = main_repo(&base.join("repo"), "ref: refs/heads/main\n");
        let info = read_repo(&git).unwrap();
        assert_eq!(info.worktrees.len(), 1);
        assert!(info.worktrees[0].is_main);
    }

    #[test]
    fn read_repo_drops_prunable_and_broken_entries() {
        let (_guard, base) = tmp();
        let git = main_repo(&base.join("repo"), "ref: refs/heads/main\n");
        // Checkout gone: `.git` target does not exist.
        linked(&git, "gone", &base.join("gone"), "ref: refs/heads/x\n");
        fs::remove_dir_all(base.join("gone")).unwrap();
        // Admin dir without a `gitdir` file, and one with an empty `gitdir`.
        write(&git.join("worktrees/nofile/HEAD"), "ref: refs/heads/y\n");
        write(&git.join("worktrees/empty/gitdir"), "\n");
        // A stray file is not an admin dir.
        write(&git.join("worktrees/stray"), "x");
        let info = read_repo(&git).unwrap();
        assert_eq!(info.worktrees.len(), 1);
        assert!(info.worktrees[0].is_main);
    }

    #[test]
    fn read_repo_rejects_missing_and_non_git_paths() {
        let (_guard, base) = tmp();
        fs::create_dir_all(base.join("plain")).unwrap();
        let plain = read_repo(&base.join("plain")).unwrap_err();
        assert_eq!(plain.kind(), io::ErrorKind::InvalidInput);
        let missing = read_repo(&base.join("missing")).unwrap_err();
        assert_eq!(missing.kind(), io::ErrorKind::NotFound);
    }

    #[test]
    fn read_repo_fails_when_worktrees_is_not_a_directory() {
        let (_guard, base) = tmp();
        let git = main_repo(&base.join("repo"), "ref: refs/heads/main\n");
        write(&git.join("worktrees"), "not a dir");
        assert!(read_repo(&git).is_err());
    }

    #[test]
    fn malformed_or_dangling_dot_git_is_not_a_checkout() {
        let (_guard, base) = tmp();
        let wt = base.join("wt");
        write(&wt.join(".git"), "hello\n");
        assert_eq!(discover(&wt), None, "no gitdir line");
        write(&wt.join(".git"), "gitdir:\n");
        assert_eq!(discover(&wt), None, "empty gitdir");
        write(&wt.join(".git"), "gitdir: ../nowhere\n");
        assert_eq!(discover(&wt), None, "target missing");
    }

    #[test]
    fn linked_admin_without_a_usable_commondir_is_not_a_checkout() {
        let (_guard, base) = tmp();
        let git = main_repo(&base.join("repo"), "ref: refs/heads/main\n");
        let wt = base.join("wt");
        let admin = linked(&git, "w", &wt, "ref: refs/heads/b\n");
        fs::remove_file(admin.join("commondir")).unwrap();
        assert_eq!(discover(&wt), None, "commondir missing");
        fs::create_dir_all(admin.join("commondir")).unwrap();
        assert_eq!(discover(&wt), None, "commondir unreadable");
    }

    #[test]
    fn bare_repositories_are_not_checkouts() {
        let (_guard, base) = tmp();
        let bare = base.join("bare.git");
        write(&bare.join("HEAD"), "ref: refs/heads/main\n");
        assert_eq!(discover(&bare), None, "bare dir itself");
        let wt = base.join("wt");
        linked(&bare, "w", &wt, "ref: refs/heads/b\n");
        assert_eq!(discover(&wt), None, "worktree of a bare repo");
        assert!(read_repo(&bare).is_err());
    }

    #[test]
    fn scan_maps_cwds_and_shares_one_repo() {
        let (_guard, base) = tmp();
        let repo = base.join("repo");
        let git = main_repo(&repo, "ref: refs/heads/main\n");
        let wt = base.join("wt1");
        linked(&git, "w1", &wt, "ref: refs/heads/f\n");
        fs::create_dir_all(repo.join("src")).unwrap();
        fs::create_dir_all(base.join("plain")).unwrap();
        let cwd = |p: &Path| p.to_string_lossy().into_owned();
        let cwds = vec![
            cwd(&repo.join("src")),
            cwd(&wt),
            cwd(&repo.join("src")),
            cwd(&base.join("plain")),
        ];
        let got = scan(&cwds);
        assert_eq!(got.repos.len(), 1);
        assert_eq!(got.checkouts.len(), 2);
        assert!(!got.checkouts.contains_key(&cwd(&base.join("plain"))));
        assert!(got.checkouts[&cwd(&repo.join("src"))].is_main);
        assert!(!got.checkouts[&cwd(&wt)].is_main);
        assert_eq!(got.repos[&git].worktrees.len(), 2);
    }

    #[test]
    fn scan_of_nothing_is_empty() {
        assert_eq!(scan(&[]), Scan::default());
        assert_eq!(scan(&["/no/such/dir/anywhere".to_owned()]), Scan::default());
    }

    #[test]
    fn scan_drops_checkouts_of_a_repo_that_cannot_be_read() {
        let (_guard, base) = tmp();
        let git = main_repo(&base.join("repo"), "ref: refs/heads/main\n");
        write(&git.join("worktrees"), "not a dir");
        let cwd = base.join("repo").to_string_lossy().into_owned();
        let got = scan(&[cwd]);
        assert!(got.repos.is_empty());
        assert!(got.checkouts.is_empty());
    }
}
