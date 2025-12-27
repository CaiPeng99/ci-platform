use std::env;
// use std::process::Command;
use std::time::{SystemTime, UNIX_EPOCH};
use tokio::time::{sleep, Duration};
use tonic::transport::Channel;
use tonic::Request;

// Include the generated proto code
pub mod runner {
    tonic::include_proto!("runner.v1");
}

// use runner::{
//     runner_gateway_client::RunnerGatewayClient, HeartbeatRequest, HeartbeatResponse,
//     JobFinished, JobSpec, LeaseJobRequest, LeaseJobResponse, LongChunk, RegisterRunnerRequest,
//     RegisterRunnerResponse, ReportAck, RunnerEvent, StepFinished, StepStarted,
// };

use runner::{
    runner_gateway_client::RunnerGatewayClient, 
    JobFinished, JobSpec, LeaseJobRequest, LongChunk, RegisterRunnerRequest,
    RunnerEvent, StepFinished, StepStarted,
};
// Removed: HeartbeatRequest, HeartbeatResponse, LeaseJobResponse, RegisterRunnerResponse, ReportAck

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

    println!("🚀 Executing job {} with {} steps", job_id, steps.len());

    // Connect to Docker daemon
    let docker = Docker::connect_with_local_defaults()?;
    
    // Create event stream
    let (tx, rx) = tokio::sync::mpsc::channel(100);
    let outbound = tokio_stream::wrappers::ReceiverStream::new(rx);

    let response_future = client.report_event(Request::new(outbound));

    let mut job_success = true;

    // Execute steps in Docker containers
    for step in steps {
        println!("  📝 Step {}: {}", step.step_id, step.name);
        println!("  🔧 Command: {}", step.command);  // ✅ ADD: Show command

        // Report step started
        tx.send(RunnerEvent {
            runner_id: runner_id.to_string(),
            job_id: job_id.clone(),
            event: Some(runner::runner_event::Event::StepStarted(StepStarted {
                step_id: step.step_id.clone(),
                ts_unix_ms: current_timestamp_ms(),
            })),
        })
        .await?;

        // Execute command in Docker container
        match execute_in_docker(&docker, &step.command, &job_id).await {
            Ok((output, exit_code)) => {
                // ✅ ADD: Print output for debugging
                println!("  📄 Output: {}", output);
                println!("  🔢 Exit code: {}", exit_code);

                // Send log output
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
                    println!("  ❌ Step {} failed", step.name);
                } else {
                    println!("  ✅ Step {} completed", step.name);
                }

                // Report step finished
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
                
                
                // Report failure
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
                
                job_success = false;
                
                // Report step as failed
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

        // Stop if step failed
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

    Ok(())
}
//         // Send log output
//         tx.send(RunnerEvent {
//             runner_id: runner_id.to_string(),
//             job_id: job_id.clone(),
//             event: Some(runner::runner_event::Event::LongChunk(LongChunk {
//                 step_id: step.step_id.clone(),
//                 content: output_str,
//                 ts_unix_ms: current_timestamp_ms(),
//             })),
//         })
//         .await?;

//         // Determine step result
//         let exit_code = output.status.code().unwrap_or(1);
//         let status = if output.status.success() {
//             "success"
//         } else {
//             "failed"
//         };
//         let error_message = if output.status.success() {
//             String::new()
//         } else {
//             format!("Command failed with exit code {}", exit_code)
//         };

//         if !output.status.success() {
//             job_success = false;
//             println!("  ❌ Step {} failed", step.name);
//         } else {
//             println!("  ✅ Step {} completed", step.name);
//         }

//         // Report step finished
//         tx.send(RunnerEvent {
//             runner_id: runner_id.to_string(),
//             job_id: job_id.clone(),
//             event: Some(runner::runner_event::Event::StepFinished(StepFinished {
//                 step_id: step.step_id.clone(),
//                 exit_code: exit_code as i32,
//                 status: status.to_string(),
//                 error_message,
//                 ts_unix_ms: current_timestamp_ms(),
//             })),
//         })
//         .await?;

//         // Stop if step failed
//         if !job_success {
//             break;
//         }
//     }

//     // Report job finished
//     let job_status = if job_success { "success" } else { "failed" };

//     tx.send(RunnerEvent {
//         runner_id: runner_id.to_string(),
//         job_id: job_id.clone(),
//         event: Some(runner::runner_event::Event::JobFinished(JobFinished {
//             status: job_status.to_string(),
//             error_message: String::new(),
//             ts_unix_ms: current_timestamp_ms(),
//         })),
//     })
//     .await?;

//     // Close the sender to signal completion
//     drop(tx);

//     // Wait for response
//     // let response = response_future.await?;
//     let _response = response_future.await?; 
//     println!("📊 Job {} completed with status: {}", job_id, job_status);

//     Ok(())
// }

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
) -> Result<(String, i32), Box<dyn std::error::Error>> {
    let container_name = format!("ci-job-{}-{}", job_id, current_timestamp_ms());
    let image = "alpine:latest";

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
       
    // Container configuration
    let config = Config {
        image: Some(image),  // Default image (can be configurable)
        cmd: Some(vec!["sh", "-c", command]),
        host_config: Some(HostConfig {
            auto_remove: Some(false),   // Don't auto-remove, we'll do it manually
            memory: Some(2_000_000_000),  // 2GB limit
            nano_cpus: Some(1_000_000_000),  // 1 CPU limit
            ..Default::default()
        }),
        attach_stdout: Some(true),
        attach_stderr: Some(true),
        ..Default::default()
    };

    // At the top of execute_in_docker
    // let image = env::var("DOCKER_IMAGE").unwrap_or_else(|_| "alpine:latest".to_string());
    // let memory_limit = env::var("MEMORY_LIMIT")
    //     .unwrap_or_else(|_| "2000000000".to_string())
    //     .parse::<i64>()
    //     .unwrap_or(2_000_000_000);
    // let cpu_limit = env::var("CPU_LIMIT")
    //     .unwrap_or_else(|_| "1000000000".to_string())
    //     .parse::<i64>()
    //     .unwrap_or(1_000_000_000);


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

    // Collect logs
    // let mut log_stream = docker.logs(
    //     &container_id,
    //     Some(LogsOptions::<String> {
    //         follow: true,
    //         stdout: true,
    //         stderr: true,
    //         ..Default::default()
    //     }),
    // );

    // let mut output = String::new();
    // while let Some(log) = log_stream.next().await {
    //     match log {
    //         Ok(log_output) => {
    //             output.push_str(&log_output.to_string());
    //         }
    //         Err(e) => {
    //             eprintln!("Log stream error: {}", e);
    //             break;
    //         }
    //     }
    // }

    // // Wait for container to finish and get exit code
    // let wait_result = docker.wait_container(&container_id, None::<bollard::container::WaitContainerOptions<String>>).next().await;
    
    // let exit_code = match wait_result {
    //     Some(Ok(result)) => result.status_code,
    //     _ => 1,
    // };

    // // Stream logs and wait for completion in parallel
    // let log_task = {
    //     let docker = docker.clone();
    //     let container_id = container_id.clone();
    //     tokio::spawn(async move {
    //         let mut log_stream = docker.logs(
    //             &container_id,
    //             Some(LogsOptions::<String> {
    //                 follow: true,
    //                 stdout: true,
    //                 stderr: true,
    //                 ..Default::default()
    //             }),
    //         );

    //         let mut output = String::new();
    //         while let Some(log) = log_stream.next().await {
    //             match log {
    //                 Ok(log_output) => {
    //                     output.push_str(&log_output.to_string());
    //                 }
    //                 Err(e) => {
    //                     eprintln!("Log stream error: {}", e);
    //                     break;
    //                 }
    //             }
    //         }
    //         output
    //     })
    // };

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



    // Get collected logs
    // let output = log_task.await.unwrap_or_default();


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

