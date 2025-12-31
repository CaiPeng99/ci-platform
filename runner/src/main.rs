use std::env;
// use std::process::Command;
use std::time::{SystemTime, UNIX_EPOCH};
use tokio::time::{sleep, Duration};
use tonic::transport::Channel;
use tonic::Request;

// use std::fs::File;
// use std::io::Write;
// use zip::write::FileOptions;
// use zip::ZipWriter;
// use walkdir::WalkDir;

// Include the generated proto code
pub mod runner {
    tonic::include_proto!("runner.v1");
}

use runner::{
    runner_gateway_client::RunnerGatewayClient, 
    JobFinished, JobSpec, LeaseJobRequest, LongChunk, RegisterRunnerRequest,
    RunnerEvent, StepFinished, StepStarted,
};

use bollard::Docker;
use bollard::container::{
    Config, CreateContainerOptions, RemoveContainerOptions, StartContainerOptions,
    LogsOptions, WaitContainerOptions,
};

use bollard::models::HostConfig;
use futures::stream::StreamExt;
use futures::stream::TryStreamExt;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Configuration
    let control_plane_addr = env::var("CONTROL_PLANE_ADDR")
        .unwrap_or_else(|_| "http://localhost:9090".to_string());
    let runner_id = env::var("RUNNER_ID").unwrap_or_else(|_| "rust-runner-1".to_string());

    println!("🦀 Starting Rust runner: {}", runner_id);
    println!("📡 Connecting to control plane: {}", control_plane_addr);

    // Connect to control plane
    let channel = Channel::from_shared(control_plane_addr.clone())?
        .connect()
        .await?;
    let mut client = RunnerGatewayClient::new(channel);

    // Register runner
    let register_request = Request::new(RegisterRunnerRequest {
        runner_name: runner_id.clone(),
        labels: vec!["linux".to_string(), "rust".to_string()],
    });

    client.register_runner(register_request).await?;
    println!("✅ Runner registered");

    // Main loop: request and execute jobs
    loop {
        println!("🔍 Requesting job from control plane...");

        let lease_request = Request::new(LeaseJobRequest {
            runner_id: runner_id.clone(),
            labels: vec!["linux".to_string(), "rust".to_string()],
        });

        match client.lease_job(lease_request).await {
            Ok(response) => {
                let lease_response = response.into_inner();

                if !lease_response.has_job {
                    println!("⏳ No jobs available, waiting...");
                    sleep(Duration::from_secs(5)).await;
                    continue;
                }

                if let Some(job_spec) = lease_response.job_spec {
                    println!(
                        "✅ Got job: {} (ID: {})",
                        job_spec.job_name, job_spec.job_id
                    );

                    // Reconnect for streaming (streaming requires fresh connection)
                    let channel = Channel::from_shared(control_plane_addr.clone())?
                        .connect()
                        .await?;
                    let mut stream_client = RunnerGatewayClient::new(channel);

                    if let Err(e) = execute_job(&mut stream_client, &runner_id, job_spec).await {
                        eprintln!("❌ Failed to execute job: {}", e);
                    }
                }
            }
            Err(e) => {
                eprintln!("❌ Failed to lease job: {}", e);
                sleep(Duration::from_secs(5)).await;
            }
        }
    }
}



async fn execute_job(
    client: &mut RunnerGatewayClient<Channel>,
    runner_id: &str,
    job: JobSpec,
) -> Result<(), Box<dyn std::error::Error>> {
    let job_id = job.job_id.clone();
    let steps = job.steps.clone();
    let repo_path = job.repo_path.clone();

    println!("🚀 Executing job {} with {} steps", job_id, steps.len());
    println!("📁 Repo path: {}", repo_path);

    let docker = Docker::connect_with_local_defaults()?;
    
    let mut job_success = true;
    let workspace_path = format!("/tmp/ci-workspace-{}", job_id);  // ✅ Define here at top

    std::fs::create_dir_all(&workspace_path)?;
    println!("  📁 Created workspace: {}", workspace_path);

    let (tx, rx) = tokio::sync::mpsc::channel(100);
    let outbound = tokio_stream::wrappers::ReceiverStream::new(rx);

    let response_future = client.report_event(Request::new(outbound));


    // Execute steps
    for step in steps {
        println!("  📝 Step {}: {}", step.step_id, step.name);
        println!("  🔧 Command: {}", step.command);

        tx.send(RunnerEvent {
            runner_id: runner_id.to_string(),
            job_id: job_id.clone(),
            event: Some(runner::runner_event::Event::StepStarted(StepStarted {
                step_id: step.step_id.clone(),
                ts_unix_ms: current_timestamp_ms(),
            })),
        })
        .await?;

        match execute_in_docker(&docker, &step.command, &job_id, &repo_path, &workspace_path).await {
            Ok((output, exit_code)) => {
                println!("  📄 Output: {}", output);
                println!("  🔢 Exit code: {}", exit_code);
                
                tx.send(RunnerEvent {
                    runner_id: runner_id.to_string(),
                    job_id: job_id.clone(),
                    event: Some(runner::runner_event::Event::LongChunk(LongChunk {
                        step_id: step.step_id.clone(),
                        content: output.clone(),
                        ts_unix_ms: current_timestamp_ms(),
                    })),
                })
                .await?;

                let status = if exit_code == 0 { "success" } else { "failed" };
                let error_message = if exit_code != 0 {
                    format!("Command failed with exit code {}", exit_code)
                } else {
                    String::new()
                };

                if exit_code != 0 {
                    job_success = false;
                    println!("  ❌ Step {} failed with exit code {}", step.name, exit_code);
                } else {
                    println!("  ✅ Step {} completed", step.name);
                }

                tx.send(RunnerEvent {
                    runner_id: runner_id.to_string(),
                    job_id: job_id.clone(),
                    event: Some(runner::runner_event::Event::StepFinished(StepFinished {
                        step_id: step.step_id.clone(),
                        exit_code,
                        status: status.to_string(),
                        error_message,
                        ts_unix_ms: current_timestamp_ms(),
                    })),
                })
                .await?;
            }
            Err(e) => {
                eprintln!("  ❌ Docker execution failed: {}", e);
                
                tx.send(RunnerEvent {
                    runner_id: runner_id.to_string(),
                    job_id: job_id.clone(),
                    event: Some(runner::runner_event::Event::LongChunk(LongChunk {
                        step_id: step.step_id.clone(),
                        content: format!("❌ Docker error: {}\n", e),
                        ts_unix_ms: current_timestamp_ms(),
                    })),
                })
                .await?;
                
                job_success = false;
                
                tx.send(RunnerEvent {
                    runner_id: runner_id.to_string(),
                    job_id: job_id.clone(),
                    event: Some(runner::runner_event::Event::StepFinished(StepFinished {
                        step_id: step.step_id.clone(),
                        exit_code: 1,
                        status: "failed".to_string(),
                        error_message: format!("Docker error: {}", e),
                        ts_unix_ms: current_timestamp_ms(),
                    })),
                })
                .await?;
            }
        }

        if !job_success {
            break;
        }
    }

    // Report job finished
    let job_status = if job_success { "success" } else { "failed" };

    tx.send(RunnerEvent {
        runner_id: runner_id.to_string(),
        job_id: job_id.clone(),
        event: Some(runner::runner_event::Event::JobFinished(JobFinished {
            status: job_status.to_string(),
            error_message: String::new(),
            ts_unix_ms: current_timestamp_ms(),
        })),
    })
    .await?;

    drop(tx);
    let _response = response_future.await?;
    println!("📊 Job {} completed with status: {}", job_id, job_status);

    // ✅ Upload artifacts if job succeeded
    if job_success {
        // Debug: Check what's in workspace before upload
        println!("  🔍 Checking workspace before upload...");
        if let Ok(entries) = std::fs::read_dir(&workspace_path) {
            for entry in entries.flatten() {
                println!("    Found: {:?}", entry.path());
            }
        } else {
            println!("    ⚠️  Workspace doesn't exist!");
        }

        // let control_plane_url = std::env::var("CONTROL_PLANE_ADDR")
        //     .unwrap_or_else(|_| "http://localhost:8080".to_string());
        
        let control_plane_http = std::env::var("CONTROL_PLANE_HTTP_ADDR")
            .unwrap_or_else(|_| "http://localhost:8080".to_string());

        if let Err(e) = upload_artifacts(&control_plane_http, &job_id, &workspace_path).await {
            eprintln!("  ⚠️  Failed to upload artifacts: {}", e);
        }
        
        // ✅ OR: Build HTTP URL from gRPC addr
        // let http_url = if control_plane_url.contains(":9090") {
        //     control_plane_url.replace(":9090", ":8080")
        // } else {
        //     control_plane_url
        // };

    
            // // Use the workspace directory instead of repo_path
            // let workspace_path = format!("/tmp/ci-workspace-{}", job_id);
        
    //    if let Err(e) = upload_artifacts(&control_plane_url, &job_id, &workspace_path).await {
    //         eprintln!("  ⚠️  Failed to upload artifacts: {}", e);
    //     }
    }

    // Cleanup workspace
    if let Err(e) = std::fs::remove_dir_all(&workspace_path) {
        eprintln!("Failed to cleanup workspace {}: {}", workspace_path, e);
    }

    Ok(())  // ✅ This is now at the end
}

fn current_timestamp_ms() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_millis() as i64
}

async fn execute_in_docker(
    docker: &Docker,
    command: &str,
    job_id: &str,
    repo_path: &str, 
    workspace_path: &str,
) -> Result<(String, i32), Box<dyn std::error::Error>> {
    let container_name = format!("ci-job-{}-{}", job_id, current_timestamp_ms());
    // let image = "alpine:latest";
    let image = "golang:1.21-alpine";

    // ✅ ADD: Pull image if not present
    match docker.inspect_image(image).await {
        Err(_) => {
            println!("  📦 Pulling image {}...", image);
            use bollard::image::CreateImageOptions;
            // use futures::stream::TryStreamExt;
            
            let mut stream = docker.create_image(
                Some(CreateImageOptions {
                    from_image: image,
                    ..Default::default()
                }),
                None,
                None,
            );

            // Wait for pull to complete
            while let Some(result) = stream.try_next().await? {
                if let Some(status) = result.status {
                    if status.contains("Downloaded") || status.contains("Pull complete") {
                        println!("  ✅ Image pulled successfully");
                    }
                }
            }
        }
        Ok(_) => {
            println!("  ✅ Image {} already exists", image);
        }
    }

    // let absolute_repo_path = std::fs::canonicalize(repo_path)?;

    // ✅ FIX: Handle relative paths better
    let absolute_repo_path = if std::path::Path::new(repo_path).is_absolute() {
        std::path::PathBuf::from(repo_path)
    } else {
        // Get current working directory and join with relative path
        let current_dir = std::env::current_dir()?;
        let resolved = current_dir.parent()  // Go up from runner/ to ci-platform/
            .ok_or("Cannot get parent directory")?
            .join(repo_path);
        
        std::fs::canonicalize(&resolved)?
    };

    let repo_path_str = absolute_repo_path
        .to_str()
        .ok_or("Invalid repo path")?;

    println!("  📁 Mounting: {} -> /work", repo_path_str);

    // ✅ CREATE COMMAND STRING BEFORE CONFIG
    let full_command = format!("cd /work && {}", command);

    let workspace_dir = format!("/tmp/ci-workspace-{}", job_id);
    std::fs::create_dir_all(&workspace_dir)?;
    println!("  📁 Mounting: {} -> /work (read-only)", repo_path_str);
    println!("  📁 Workspace: {} -> /workspace (writable)", workspace_dir);

    // Container configuration
    let config = Config {
        image: Some(image),  // Default image (can be configurable)
        // cmd: Some(vec!["sh", "-c", command]),
        cmd: Some(vec!["sh", "-c", &full_command]),  // Use reference to full_command
        working_dir: Some("/work"),  // ✅ Set working directory
        host_config: Some(HostConfig {
            auto_remove: Some(false),   // Don't auto-remove, we'll do it manually
            memory: Some(2_000_000_000),  // 2GB limit
            nano_cpus: Some(1_000_000_000),  // 1 CPU limit
            binds: Some(vec![
                format!("{}:/work:ro", repo_path_str),           // ✅ Source code (read-only)
                format!("{}:/workspace", workspace_path),          // ✅ Workspace (writable)
            ]),
            ..Default::default()
        }),
        attach_stdout: Some(true),
        attach_stderr: Some(true),
        ..Default::default()
    };


    // Create container
    let options = CreateContainerOptions {
        name: &container_name,
        platform: None,
    };

    let container = docker.create_container(Some(options), config).await?;
    let container_id = container.id;

    // Start container
    docker
        .start_container(&container_id, None::<StartContainerOptions<String>>)
        .await?;

    // Wait for container to exit
    let mut wait_stream = docker.wait_container(
        &container_id,
        None::<WaitContainerOptions<String>>,
    );

    let exit_code = match wait_stream.next().await {
        Some(Ok(result)) => result.status_code,
        Some(Err(e)) => {
            eprintln!("Wait error: {}", e);
            1
        }
        None => 1,
    };

    // Now get logs (container is stopped but not removed)
    let mut log_stream = docker.logs(
        &container_id,
        Some(LogsOptions::<String> {
            follow: false,  // Container is done, just get all logs
            stdout: true,
            stderr: true,
            ..Default::default()
        }),
    );

    let mut output = String::new();
    while let Some(log) = log_stream.next().await {
        match log {
            Ok(log_output) => {
                output.push_str(&log_output.to_string());
            }
            Err(e) => {
                eprintln!("Log retrieval error: {}", e);
                break;
            }
        }
    }



    // Container is auto-removed due to auto_remove: true
    // But if it fails, manually remove
    let _ = docker.remove_container(
        &container_id,
        Some(RemoveContainerOptions {
            force: true,
            ..Default::default()
        }),
    ).await;

    Ok((output, exit_code as i32))
}


async fn upload_artifacts(
    control_plane_addr: &str,
    job_id: &str,
    workspace_path: &str,
) -> Result<(), Box<dyn std::error::Error>> {
    println!("  📦 Preparing artifacts...");
    
    // Create zip of workspace
    let zip_path = format!("/tmp/artifacts-job-{}.zip", job_id);
    create_workspace_zip(workspace_path, &zip_path)?;
    
    // Check file size
    let metadata = std::fs::metadata(&zip_path)?;
    let size = metadata.len();
    
    if size == 0 {
        println!("  ℹ️  No artifacts to upload");
        std::fs::remove_file(&zip_path)?;
        return Ok(());
    }
    
    println!("  📦 Artifacts size: {} bytes", size);
    
    // Step 1: Get presigned upload URL
    let client = reqwest::Client::new();  // ✅ Async client
    let filename = format!("artifacts-job-{}.zip", job_id);
    
    let url_request = serde_json::json!({
        "job_id": job_id.parse::<i64>()?,
        "filename": filename
    });
    
    let url_response: serde_json::Value = client
        .post(&format!("{}/artifacts/upload-url", control_plane_addr))
        .json(&url_request)
        .send()
        .await?
        .json()
        .await?;
    
    let upload_url = url_response["upload_url"]
        .as_str()
        .ok_or("Missing upload_url")?;
    let object_key = url_response["object_key"]
        .as_str()
        .ok_or("Missing object_key")?;
    
    println!("  📤 Uploading to MinIO...");
    
    // Step 2: Upload to MinIO
    let file_bytes = tokio::fs::read(&zip_path).await?;
    let upload_response = client
        .put(upload_url)
        .header("Content-Type", "application/zip")
        .body(file_bytes)
        .send()
        .await?;
    
    if !upload_response.status().is_success() {
        return Err(format!("Upload failed: {}", upload_response.status()).into());
    }
    
    // Step 3: Notify control plane
    let complete_request = serde_json::json!({
        "job_id": job_id.parse::<i64>()?,
        "filename": filename,
        "object_key": object_key,
        "size_bytes": size,
        "content_type": "application/zip"
    });
    
    client
        .post(&format!("{}/artifacts/complete", control_plane_addr))
        .json(&complete_request)
        .send()
        .await?;
    
    println!("  ✅ Artifacts uploaded successfully");
    
    // Cleanup
    tokio::fs::remove_file(&zip_path).await?;
    
    Ok(())
}

fn create_workspace_zip(workspace: &str, output_path: &str) -> Result<(), Box<dyn std::error::Error>> {
    use std::fs::File;
    use zip::write::FileOptions;
    use zip::ZipWriter;
    use walkdir::WalkDir;

    println!("    🔍 Scanning workspace: {}", workspace);
    
    let file = File::create(output_path)?;
    let mut zip = ZipWriter::new(file);
    let options = FileOptions::default()
        .compression_method(zip::CompressionMethod::Deflated);
    
    let mut file_count = 0;
    
    // Walk workspace and add files
    for entry in WalkDir::new(workspace)
        .into_iter()
        .filter_map(|e| e.ok())
        .filter(|e| e.file_type().is_file())
    {
        let path = entry.path();
        let name = match path.strip_prefix(workspace) {
            Ok(n) => n,
            Err(_) => continue,
        };
        
        // Skip certain files
        let name_str = name.to_string_lossy();
        if name_str.starts_with(".git") || 
           name_str.starts_with(".ci") ||
           name_str.contains("target/") {
            continue;
        }

        println!("    📄 Adding: {}", name.display());
        file_count += 1;
        zip.start_file(name.to_string_lossy().to_string(), options)?;
        let mut f = File::open(path)?;
        std::io::copy(&mut f, &mut zip)?;
    }
    
    zip.finish()?;

    println!("    ✓ Added {} files to zip", file_count); 
    
    if file_count == 0 {
        // Create empty marker file so zip isn't empty
        std::fs::remove_file(output_path)?;
        File::create(output_path)?;
    }
    
    Ok(())
}

    


