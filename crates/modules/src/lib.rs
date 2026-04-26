// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Local module catalog. Mirrors `internal/modules/catalog.go` from the Go
//! repo: discovers `.rillan/modules/*/module.yaml`, normalizes adapter
//! commands relative to each module root, validates the manifest, and applies
//! trust filtering against the system policy.

use std::collections::BTreeSet;
use std::fs;
use std::io;
use std::path::{Path, PathBuf};

use rillan_config::{
    LlmProviderConfig, McpServerConfig, SystemConfig, TrustedModulePolicy, LLM_TRANSPORT_HTTP,
    LLM_TRANSPORT_STDIO,
};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use thiserror::Error;

const MANIFEST_FILE_NAME: &str = "module.yaml";

/// On-disk manifest shape. Field order matches the upstream Go struct.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct Manifest {
    #[serde(default)]
    pub id: String,
    #[serde(default)]
    pub display_name: String,
    #[serde(default)]
    pub version: String,
    #[serde(default)]
    pub entrypoint: Vec<String>,
    #[serde(default)]
    pub llm_adapters: Vec<LlmProviderConfig>,
    #[serde(default)]
    pub mcp_servers: Vec<McpServerConfig>,
    #[serde(default)]
    pub lsp_servers: Vec<LspServerConfig>,
}

/// One LSP server description embedded in a module manifest.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct LspServerConfig {
    #[serde(default)]
    pub id: String,
    #[serde(default)]
    pub command: Vec<String>,
    #[serde(default)]
    pub languages: Vec<String>,
}

/// Resolved module after normalization.
#[derive(Debug, Clone, PartialEq)]
pub struct LoadedModule {
    pub id: String,
    pub display_name: String,
    pub version: String,
    pub root_path: PathBuf,
    pub manifest_sha256: String,
    pub manifest_path: PathBuf,
    pub entrypoint: Vec<String>,
    pub llm_adapters: Vec<LlmProviderConfig>,
    pub mcp_servers: Vec<McpServerConfig>,
    pub lsp_servers: Vec<LspServerConfig>,
}

/// Set of modules discovered under `modules_dir`.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct Catalog {
    pub modules_dir: PathBuf,
    pub modules: Vec<LoadedModule>,
}

/// Errors raised by catalog discovery or filtering.
#[derive(Debug, Error)]
pub enum Error {
    #[error("read modules dir: {0}")]
    ReadDir(#[source] io::Error),
    #[error("read module manifest {path}: {source}")]
    ReadManifest {
        path: PathBuf,
        #[source]
        source: io::Error,
    },
    #[error("parse module manifest {path}: {source}")]
    ParseManifest {
        path: PathBuf,
        #[source]
        source: serde_yaml::Error,
    },
    #[error("module manifest {path} id must not be empty")]
    EmptyId { path: PathBuf },
    #[error("module {0:?} version must not be empty")]
    EmptyVersion(String),
    #[error("module {0:?} entrypoint must not be empty")]
    EmptyEntrypoint(String),
    #[error("module {module:?} llm_adapters.id must not be empty")]
    LlmAdapterIdEmpty { module: String },
    #[error("module {module:?} llm adapter {id:?} declared more than once")]
    LlmAdapterDuplicate { module: String, id: String },
    #[error("module {module:?} llm adapter {id:?} backend must not be empty")]
    LlmAdapterBackendEmpty { module: String, id: String },
    #[error(
        "module {module:?} llm adapter {id:?} endpoint must not be empty when transport is \"http\""
    )]
    LlmAdapterEndpointEmpty { module: String, id: String },
    #[error(
        "module {module:?} llm adapter {id:?} command must not be empty when transport is \"stdio\""
    )]
    LlmAdapterCommandEmpty { module: String, id: String },
    #[error("module {module:?} llm adapter {id:?} transport must be \"http\" or \"stdio\"")]
    LlmAdapterTransportInvalid { module: String, id: String },
    #[error("module {module:?} mcp_servers.id must not be empty")]
    McpServerIdEmpty { module: String },
    #[error("module {module:?} mcp server {id:?} declared more than once")]
    McpServerDuplicate { module: String, id: String },
    #[error(
        "module {module:?} mcp server {id:?} endpoint must not be empty when transport is \"http\""
    )]
    McpServerEndpointEmpty { module: String, id: String },
    #[error(
        "module {module:?} mcp server {id:?} command must not be empty when transport is \"stdio\""
    )]
    McpServerCommandEmpty { module: String, id: String },
    #[error("module {module:?} mcp server {id:?} transport must be \"http\" or \"stdio\"")]
    McpServerTransportInvalid { module: String, id: String },
    #[error("module {module:?} lsp_servers.id must not be empty")]
    LspServerIdEmpty { module: String },
    #[error("module {module:?} lsp server {id:?} declared more than once")]
    LspServerDuplicate { module: String, id: String },
    #[error("module {module:?} lsp server {id:?} command must not be empty")]
    LspServerCommandEmpty { module: String, id: String },
    #[error("module {id:?} declared more than once in {first} and {second}")]
    DuplicateModuleId {
        id: String,
        first: PathBuf,
        second: PathBuf,
    },
    #[error("enabled module {0:?} not found in {1}")]
    EnabledNotFound(String, PathBuf),
    #[error("project root must not be empty")]
    EmptyProjectRoot,
    #[error("canonicalize project root {0}: {1}")]
    Canonicalize(PathBuf, #[source] io::Error),
    #[error("enabled module {id:?} is not trusted for repo {root}")]
    UntrustedModule { id: String, root: PathBuf },
    #[error("enabled module {0:?} requires explicit stdio trust")]
    StdioTrustRequired(String),
}

/// Returns the default modules directory for a given project config path.
#[must_use]
pub fn default_project_modules_dir(project_config_path: &Path) -> PathBuf {
    if project_config_path.as_os_str().is_empty() {
        return PathBuf::from(".rillan").join("modules");
    }
    project_config_path
        .parent()
        .map_or_else(|| PathBuf::from("modules"), |p| p.join("modules"))
}

/// Returns the project root for a given project config path
/// (`<project>/.rillan/project.yaml` → `<project>`).
#[must_use]
pub fn project_root_from_config_path(project_config_path: &Path) -> PathBuf {
    if project_config_path.as_os_str().is_empty() {
        return PathBuf::new();
    }
    project_config_path
        .parent()
        .and_then(Path::parent)
        .map_or_else(PathBuf::new, Path::to_path_buf)
}

/// Discovers and loads all modules under
/// `<project_config_dir>/modules/*/module.yaml`.
pub fn load_project_catalog(project_config_path: &Path) -> Result<Catalog, Error> {
    let modules_dir = default_project_modules_dir(project_config_path);
    let read_dir = match fs::read_dir(&modules_dir) {
        Ok(iter) => iter,
        Err(err) if err.kind() == io::ErrorKind::NotFound => {
            return Ok(Catalog {
                modules_dir,
                modules: Vec::new(),
            });
        }
        Err(err) => return Err(Error::ReadDir(err)),
    };

    let mut modules: Vec<LoadedModule> = Vec::new();
    let mut seen: std::collections::BTreeMap<String, PathBuf> = std::collections::BTreeMap::new();
    for entry in read_dir {
        let entry = entry.map_err(Error::ReadDir)?;
        let metadata = entry.metadata().map_err(Error::ReadDir)?;
        if !metadata.is_dir() {
            continue;
        }
        let manifest_path = entry.path().join(MANIFEST_FILE_NAME);
        let bytes = match fs::read(&manifest_path) {
            Ok(bytes) => bytes,
            Err(err) if err.kind() == io::ErrorKind::NotFound => continue,
            Err(err) => {
                return Err(Error::ReadManifest {
                    path: manifest_path,
                    source: err,
                });
            }
        };
        let manifest: Manifest =
            serde_yaml::from_slice(&bytes).map_err(|err| Error::ParseManifest {
                path: manifest_path.clone(),
                source: err,
            })?;
        let manifest_sha = manifest_sha256(&bytes);
        let root_path = manifest_path
            .parent()
            .map_or_else(PathBuf::new, Path::to_path_buf);
        let loaded =
            load_module_manifest(root_path, manifest_path.clone(), manifest, manifest_sha)?;
        if let Some(prev) = seen.get(&loaded.id) {
            return Err(Error::DuplicateModuleId {
                id: loaded.id,
                first: prev.clone(),
                second: manifest_path,
            });
        }
        seen.insert(loaded.id.clone(), manifest_path);
        modules.push(loaded);
    }

    modules.sort_by(|a, b| a.id.cmp(&b.id));
    Ok(Catalog {
        modules_dir,
        modules,
    })
}

/// Filters `catalog` to the modules whose ids appear in `enabled`. Empty
/// `enabled` yields an empty catalog (mirrors the Go behavior).
pub fn filter_enabled(catalog: Catalog, enabled: &[String]) -> Result<Catalog, Error> {
    if enabled.is_empty() {
        return Ok(Catalog {
            modules_dir: catalog.modules_dir,
            modules: Vec::new(),
        });
    }

    let by_id: std::collections::BTreeMap<&str, &LoadedModule> =
        catalog.modules.iter().map(|m| (m.id.as_str(), m)).collect();

    let mut filtered: Vec<LoadedModule> = Vec::with_capacity(enabled.len());
    let mut seen: BTreeSet<String> = BTreeSet::new();
    for raw in enabled {
        let id = raw.trim();
        if id.is_empty() {
            continue;
        }
        if !seen.insert(id.to_string()) {
            continue;
        }
        match by_id.get(id) {
            Some(module) => filtered.push((*module).clone()),
            None => {
                return Err(Error::EnabledNotFound(
                    id.to_string(),
                    catalog.modules_dir.clone(),
                ));
            }
        }
    }
    Ok(Catalog {
        modules_dir: catalog.modules_dir,
        modules: filtered,
    })
}

/// Filters `catalog` to modules trusted by the system policy for this project
/// root. Stdio adapters require explicit `allow_stdio` trust.
pub fn filter_trusted(
    catalog: Catalog,
    project_config_path: &Path,
    system: Option<&SystemConfig>,
) -> Result<Catalog, Error> {
    if catalog.modules.is_empty() {
        return Ok(catalog);
    }
    let project_root = canonical_project_root(&project_root_from_config_path(project_config_path))?;
    let mut trusted = Vec::with_capacity(catalog.modules.len());
    for module in catalog.modules.into_iter() {
        let trust = find_module_trust(system, &project_root, &module).ok_or_else(|| {
            Error::UntrustedModule {
                id: module.id.clone(),
                root: project_root.clone(),
            }
        })?;
        if module_uses_stdio(&module) && !trust.allow_stdio {
            return Err(Error::StdioTrustRequired(module.id));
        }
        trusted.push(module);
    }
    Ok(Catalog {
        modules_dir: catalog.modules_dir,
        modules: trusted,
    })
}

fn manifest_sha256(data: &[u8]) -> String {
    let mut hasher = Sha256::new();
    hasher.update(data);
    let digest = hasher.finalize();
    let mut out = String::with_capacity(digest.len() * 2);
    for byte in &digest {
        out.push_str(&format!("{byte:02x}"));
    }
    out
}

fn load_module_manifest(
    root_path: PathBuf,
    manifest_path: PathBuf,
    manifest: Manifest,
    manifest_sha256: String,
) -> Result<LoadedModule, Error> {
    let module_id = manifest.id.trim().to_string();
    if module_id.is_empty() {
        return Err(Error::EmptyId {
            path: manifest_path,
        });
    }
    let version = manifest.version.trim().to_string();
    if version.is_empty() {
        return Err(Error::EmptyVersion(module_id));
    }
    if manifest.entrypoint.is_empty() {
        return Err(Error::EmptyEntrypoint(module_id));
    }

    let llm_adapters = normalize_llm_adapters(&module_id, &root_path, manifest.llm_adapters)?;
    let mcp_servers = normalize_mcp_servers(&module_id, &root_path, manifest.mcp_servers)?;
    let lsp_servers = normalize_lsp_servers(&module_id, &root_path, manifest.lsp_servers)?;

    Ok(LoadedModule {
        id: module_id,
        display_name: manifest.display_name.trim().to_string(),
        version,
        root_path: root_path.clone(),
        manifest_sha256,
        manifest_path,
        entrypoint: normalize_command(&root_path, &manifest.entrypoint),
        llm_adapters,
        mcp_servers,
        lsp_servers,
    })
}

fn canonical_project_root(root: &Path) -> Result<PathBuf, Error> {
    if root.as_os_str().is_empty() {
        return Err(Error::EmptyProjectRoot);
    }
    root.canonicalize()
        .map_err(|err| Error::Canonicalize(root.to_path_buf(), err))
}

fn find_module_trust(
    system: Option<&SystemConfig>,
    project_root: &Path,
    module: &LoadedModule,
) -> Option<TrustedModulePolicy> {
    let system = system?;
    for trust in &system.policy.trusted_modules {
        let Ok(trusted_root) = canonical_project_root(Path::new(&trust.repo_root)) else {
            continue;
        };
        if trusted_root == project_root
            && trust.module_id.trim() == module.id
            && trust.manifest_sha256.trim() == module.manifest_sha256
        {
            return Some(trust.clone());
        }
    }
    None
}

fn module_uses_stdio(module: &LoadedModule) -> bool {
    module
        .llm_adapters
        .iter()
        .any(|adapter| adapter.transport.trim() == LLM_TRANSPORT_STDIO)
}

fn normalize_llm_adapters(
    module_id: &str,
    root_path: &Path,
    adapters: Vec<LlmProviderConfig>,
) -> Result<Vec<LlmProviderConfig>, Error> {
    let mut seen: BTreeSet<String> = BTreeSet::new();
    let mut normalized: Vec<LlmProviderConfig> = Vec::with_capacity(adapters.len());
    for mut adapter in adapters {
        let id = adapter.id.trim().to_string();
        if id.is_empty() {
            return Err(Error::LlmAdapterIdEmpty {
                module: module_id.to_string(),
            });
        }
        if !seen.insert(id.clone()) {
            return Err(Error::LlmAdapterDuplicate {
                module: module_id.to_string(),
                id,
            });
        }
        if adapter.backend.trim().is_empty() {
            return Err(Error::LlmAdapterBackendEmpty {
                module: module_id.to_string(),
                id,
            });
        }
        match adapter.transport.trim() {
            LLM_TRANSPORT_HTTP => {
                if adapter.endpoint.trim().is_empty() {
                    return Err(Error::LlmAdapterEndpointEmpty {
                        module: module_id.to_string(),
                        id,
                    });
                }
            }
            LLM_TRANSPORT_STDIO => {
                if adapter.command.is_empty() {
                    return Err(Error::LlmAdapterCommandEmpty {
                        module: module_id.to_string(),
                        id,
                    });
                }
                adapter.command = normalize_command(root_path, &adapter.command);
            }
            _ => {
                return Err(Error::LlmAdapterTransportInvalid {
                    module: module_id.to_string(),
                    id,
                });
            }
        }
        normalized.push(adapter);
    }
    Ok(normalized)
}

fn normalize_mcp_servers(
    module_id: &str,
    root_path: &Path,
    servers: Vec<McpServerConfig>,
) -> Result<Vec<McpServerConfig>, Error> {
    let mut seen: BTreeSet<String> = BTreeSet::new();
    let mut normalized: Vec<McpServerConfig> = Vec::with_capacity(servers.len());
    for mut server in servers {
        let id = server.id.trim().to_string();
        if id.is_empty() {
            return Err(Error::McpServerIdEmpty {
                module: module_id.to_string(),
            });
        }
        if !seen.insert(id.clone()) {
            return Err(Error::McpServerDuplicate {
                module: module_id.to_string(),
                id,
            });
        }
        match server.transport.trim() {
            LLM_TRANSPORT_HTTP => {
                if server.endpoint.trim().is_empty() {
                    return Err(Error::McpServerEndpointEmpty {
                        module: module_id.to_string(),
                        id,
                    });
                }
            }
            LLM_TRANSPORT_STDIO => {
                if server.command.is_empty() {
                    return Err(Error::McpServerCommandEmpty {
                        module: module_id.to_string(),
                        id,
                    });
                }
                server.command = normalize_command(root_path, &server.command);
            }
            _ => {
                return Err(Error::McpServerTransportInvalid {
                    module: module_id.to_string(),
                    id,
                });
            }
        }
        normalized.push(server);
    }
    Ok(normalized)
}

fn normalize_lsp_servers(
    module_id: &str,
    root_path: &Path,
    servers: Vec<LspServerConfig>,
) -> Result<Vec<LspServerConfig>, Error> {
    let mut seen: BTreeSet<String> = BTreeSet::new();
    let mut normalized: Vec<LspServerConfig> = Vec::with_capacity(servers.len());
    for mut server in servers {
        let id = server.id.trim().to_string();
        if id.is_empty() {
            return Err(Error::LspServerIdEmpty {
                module: module_id.to_string(),
            });
        }
        if !seen.insert(id.clone()) {
            return Err(Error::LspServerDuplicate {
                module: module_id.to_string(),
                id,
            });
        }
        if server.command.is_empty() {
            return Err(Error::LspServerCommandEmpty {
                module: module_id.to_string(),
                id,
            });
        }
        server.command = normalize_command(root_path, &server.command);
        normalized.push(server);
    }
    Ok(normalized)
}

fn normalize_command(root_path: &Path, command: &[String]) -> Vec<String> {
    if command.is_empty() {
        return Vec::new();
    }
    let mut normalized = command.to_vec();
    let first = normalized[0].trim();
    if first.is_empty() {
        return normalized;
    }
    let first_path = Path::new(first);
    // Absolute paths or bare command names (no slashes) pass through unchanged.
    if first_path.is_absolute() || !first.contains('/') {
        return normalized;
    }
    let joined = root_path.join(first_path);
    let canonical = canonicalize_lossy(&joined);
    normalized[0] = canonical.to_string_lossy().to_string();
    normalized
}

fn canonicalize_lossy(path: &Path) -> PathBuf {
    // Mirrors Go's `filepath.Clean(filepath.Join(rootPath, first))` — does not
    // touch the filesystem. We re-implement it because Rust's `Path::canonicalize`
    // requires the path to exist, but module commands point at *targets* that
    // may only appear when the module is run.
    let mut out = PathBuf::new();
    for component in path.components() {
        match component {
            std::path::Component::Prefix(p) => out.push(p.as_os_str()),
            std::path::Component::RootDir => out.push("/"),
            std::path::Component::CurDir => {}
            std::path::Component::ParentDir => {
                if !out.pop() {
                    out.push("..");
                }
            }
            std::path::Component::Normal(part) => out.push(part),
        }
    }
    if out.as_os_str().is_empty() {
        out.push(".");
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use rillan_config::{SystemPolicy, TrustedModulePolicy};

    fn write_manifest(project_path: &Path, module_dir: &str, content: &str) -> PathBuf {
        let manifest_path = default_project_modules_dir(project_path)
            .join(module_dir)
            .join(MANIFEST_FILE_NAME);
        fs::create_dir_all(manifest_path.parent().unwrap()).expect("mkdir");
        fs::write(&manifest_path, content).expect("write");
        manifest_path
    }

    #[test]
    fn load_returns_deterministic_modules() {
        let dir = tempfile::tempdir().expect("tempdir");
        let project_path = dir.path().join(".rillan").join("project.yaml");
        write_manifest(
            &project_path,
            "z-last",
            r#"id: "z-last"
version: "0.1.0"
entrypoint: ["./bin/module"]
llm_adapters:
  - id: "z-llm"
    backend: "openai_compatible"
    transport: "http"
    endpoint: "https://example.com/v1"
"#,
        );
        write_manifest(
            &project_path,
            "a-first",
            r#"id: "a-first"
version: "0.1.0"
entrypoint: ["./bin/module"]
mcp_servers:
  - id: "repo"
    transport: "stdio"
    command: ["./bin/mcp"]
lsp_servers:
  - id: "gopls"
    command: ["./bin/gopls"]
    languages: ["go"]
"#,
        );

        let catalog = load_project_catalog(&project_path).expect("load");
        assert_eq!(catalog.modules.len(), 2);
        assert_eq!(catalog.modules[0].id, "a-first");
        assert_eq!(catalog.modules[1].id, "z-last");

        let entrypoint = &catalog.modules[0].entrypoint[0];
        let basename = Path::new(entrypoint).file_name().unwrap();
        assert_eq!(basename, "module");
        assert!(
            Path::new(entrypoint).is_absolute(),
            "entrypoint {entrypoint} must be absolute",
        );
        let mcp = &catalog.modules[0].mcp_servers[0].command[0];
        assert_eq!(Path::new(mcp).file_name().unwrap(), "mcp");
        let lsp = &catalog.modules[0].lsp_servers[0].command[0];
        assert_eq!(Path::new(lsp).file_name().unwrap(), "gopls");
    }

    #[test]
    fn load_returns_empty_when_modules_dir_missing() {
        let dir = tempfile::tempdir().expect("tempdir");
        let project_path = dir.path().join(".rillan").join("project.yaml");
        let catalog = load_project_catalog(&project_path).expect("load");
        assert!(catalog.modules.is_empty());
    }

    #[test]
    fn load_rejects_invalid_manifest() {
        let dir = tempfile::tempdir().expect("tempdir");
        let project_path = dir.path().join(".rillan").join("project.yaml");
        write_manifest(
            &project_path,
            "broken",
            r#"id: "broken"
version: ""
entrypoint: ["./bin/module"]
"#,
        );
        let err = load_project_catalog(&project_path).expect_err("invalid version");
        assert!(matches!(err, Error::EmptyVersion(_)));
    }

    #[test]
    fn load_rejects_duplicate_module_ids() {
        let dir = tempfile::tempdir().expect("tempdir");
        let project_path = dir.path().join(".rillan").join("project.yaml");
        let manifest = r#"id: "shared"
version: "0.1.0"
entrypoint: ["./bin/module"]
"#;
        write_manifest(&project_path, "one", manifest);
        write_manifest(&project_path, "two", manifest);
        let err = load_project_catalog(&project_path).expect_err("duplicate ids");
        assert!(matches!(err, Error::DuplicateModuleId { .. }));
    }

    #[test]
    fn filter_enabled_returns_only_requested_modules() {
        let catalog = Catalog {
            modules_dir: PathBuf::from("/repo/.rillan/modules"),
            modules: vec![
                LoadedModule {
                    id: "alpha".into(),
                    ..stub_module()
                },
                LoadedModule {
                    id: "beta".into(),
                    ..stub_module()
                },
            ],
        };
        let filtered = filter_enabled(catalog, &["beta".to_string()]).expect("filter");
        assert_eq!(filtered.modules.len(), 1);
        assert_eq!(filtered.modules[0].id, "beta");
    }

    #[test]
    fn filter_enabled_rejects_unknown_module() {
        let catalog = Catalog {
            modules_dir: PathBuf::from("/repo/.rillan/modules"),
            modules: vec![LoadedModule {
                id: "alpha".into(),
                ..stub_module()
            }],
        };
        let err = filter_enabled(catalog, &["missing".to_string()]).expect_err("missing");
        assert!(matches!(err, Error::EnabledNotFound(_, _)));
    }

    #[test]
    fn filter_trusted_rejects_untrusted_module() {
        let dir = tempfile::tempdir().expect("tempdir");
        let project_path = dir.path().join(".rillan").join("project.yaml");
        fs::create_dir_all(project_path.parent().unwrap()).unwrap();
        fs::write(&project_path, "name: demo\n").unwrap();
        let catalog = Catalog {
            modules_dir: PathBuf::from("/repo/.rillan/modules"),
            modules: vec![LoadedModule {
                id: "alpha".into(),
                manifest_sha256: "abc123".into(),
                ..stub_module()
            }],
        };
        let err = filter_trusted(catalog, &project_path, Some(&SystemConfig::default()))
            .expect_err("untrusted");
        assert!(matches!(err, Error::UntrustedModule { .. }));
    }

    #[test]
    fn filter_trusted_rejects_stdio_module_without_stdio_trust() {
        let dir = tempfile::tempdir().expect("tempdir");
        let project_root = dir.path();
        let project_path = project_root.join(".rillan").join("project.yaml");
        fs::create_dir_all(project_path.parent().unwrap()).unwrap();
        fs::write(&project_path, "name: demo\n").unwrap();
        let canonical_root = project_root.canonicalize().unwrap();
        let catalog = Catalog {
            modules_dir: PathBuf::from("/repo/.rillan/modules"),
            modules: vec![LoadedModule {
                id: "alpha".into(),
                manifest_sha256: "abc123".into(),
                llm_adapters: vec![LlmProviderConfig {
                    id: "alpha-stdio".into(),
                    transport: LLM_TRANSPORT_STDIO.into(),
                    ..LlmProviderConfig::default()
                }],
                ..stub_module()
            }],
        };
        let system = SystemConfig {
            policy: SystemPolicy {
                trusted_modules: vec![TrustedModulePolicy {
                    repo_root: canonical_root.to_string_lossy().to_string(),
                    module_id: "alpha".into(),
                    manifest_sha256: "abc123".into(),
                    allow_stdio: false,
                }],
                ..SystemPolicy::default()
            },
            ..SystemConfig::default()
        };
        let err = filter_trusted(catalog, &project_path, Some(&system)).expect_err("stdio");
        assert!(matches!(err, Error::StdioTrustRequired(_)));
    }

    #[test]
    fn filter_trusted_accepts_trusted_http_module() {
        let dir = tempfile::tempdir().expect("tempdir");
        let project_root = dir.path();
        let project_path = project_root.join(".rillan").join("project.yaml");
        fs::create_dir_all(project_path.parent().unwrap()).unwrap();
        fs::write(&project_path, "name: demo\n").unwrap();
        let canonical_root = project_root.canonicalize().unwrap();
        let catalog = Catalog {
            modules_dir: PathBuf::from("/repo/.rillan/modules"),
            modules: vec![LoadedModule {
                id: "alpha".into(),
                manifest_sha256: "abc123".into(),
                llm_adapters: vec![LlmProviderConfig {
                    id: "alpha-http".into(),
                    transport: LLM_TRANSPORT_HTTP.into(),
                    ..LlmProviderConfig::default()
                }],
                ..stub_module()
            }],
        };
        let system = SystemConfig {
            policy: SystemPolicy {
                trusted_modules: vec![TrustedModulePolicy {
                    repo_root: canonical_root.to_string_lossy().to_string(),
                    module_id: "alpha".into(),
                    manifest_sha256: "abc123".into(),
                    allow_stdio: false,
                }],
                ..SystemPolicy::default()
            },
            ..SystemConfig::default()
        };
        let filtered = filter_trusted(catalog, &project_path, Some(&system)).expect("trusted");
        assert_eq!(filtered.modules.len(), 1);
    }

    fn stub_module() -> LoadedModule {
        LoadedModule {
            id: String::new(),
            display_name: String::new(),
            version: String::new(),
            root_path: PathBuf::new(),
            manifest_sha256: String::new(),
            manifest_path: PathBuf::new(),
            entrypoint: Vec::new(),
            llm_adapters: Vec::new(),
            mcp_servers: Vec::new(),
            lsp_servers: Vec::new(),
        }
    }
}
