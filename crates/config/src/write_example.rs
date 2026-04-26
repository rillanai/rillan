// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! `rillan init` writes the embedded example YAML scaffolding for the runtime
//! and project config files. Mirrors `internal/config/write_example.go`.

use std::fs;
use std::io;
use std::path::Path;

use crate::load::Error;

const EXAMPLE_CONFIG: &str = include_str!("../../../configs/rillan.example.yaml");
const EXAMPLE_PROJECT_CONFIG: &str = include_str!("../../../configs/project.example.yaml");

/// Writes the example runtime config to `path`.
pub fn write_example_config(path: &Path, overwrite: bool) -> Result<(), Error> {
    write_example_file(path, EXAMPLE_CONFIG, overwrite)
}

/// Writes the example project config to `path`.
pub fn write_example_project_config(path: &Path, overwrite: bool) -> Result<(), Error> {
    write_example_file(path, EXAMPLE_PROJECT_CONFIG, overwrite)
}

fn write_example_file(path: &Path, content: &str, overwrite: bool) -> Result<(), Error> {
    if let Some(parent) = path.parent() {
        if !parent.as_os_str().is_empty() {
            fs::create_dir_all(parent).map_err(Error::CreateDir)?;
        }
    }
    if !overwrite {
        match fs::metadata(path) {
            Ok(_) => {
                return Err(Error::Write(io::Error::new(
                    io::ErrorKind::AlreadyExists,
                    format!(
                        "refusing to overwrite existing file at {}; pass --force to replace it",
                        path.display(),
                    ),
                )));
            }
            Err(err) if err.kind() == io::ErrorKind::NotFound => {}
            Err(err) => return Err(Error::Write(err)),
        }
    }
    fs::write(path, content).map_err(Error::Write)?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn writes_example_config_to_tmpdir() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let path = tmp.path().join("config.yaml");
        write_example_config(&path, false).expect("write");
        let written = fs::read_to_string(&path).expect("read");
        assert!(written.contains("schema_version"));
    }

    #[test]
    fn refuses_overwrite_without_force() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let path = tmp.path().join("config.yaml");
        write_example_config(&path, false).expect("first");
        let err = write_example_config(&path, false).expect_err("must refuse");
        assert!(matches!(err, Error::Write(_)));
    }

    #[test]
    fn force_replaces_existing_file() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let path = tmp.path().join("config.yaml");
        write_example_config(&path, false).expect("first");
        write_example_config(&path, true).expect("force");
    }
}
