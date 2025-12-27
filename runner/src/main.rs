use std::env;
use std::process::Command;
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

    // Create event stream
    let (tx, rx) = tokio::sync::mpsc::channel(100);
    let outbound = tokio_stream::wrappers::ReceiverStream::new(rx);

    let response_future = client.report_event(Request::new(outbound));

    let mut job_success = true;

    // Execute steps
    for step in steps {
        println!("  📝 Step {}: {}", step.step_id, step.name);

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

        // Execute command
        let output = Command::new("sh").arg("-c").arg(&step.command).output()?;

        let output_str = String::from_utf8_lossy(&output.stdout).to_string()
            + &String::from_utf8_lossy(&output.stderr).to_string();

        // Send log output
        tx.send(RunnerEvent {
            runner_id: runner_id.to_string(),
            job_id: job_id.clone(),
            event: Some(runner::runner_event::Event::LongChunk(LongChunk {
                step_id: step.step_id.clone(),
                content: output_str,
                ts_unix_ms: current_timestamp_ms(),
            })),
        })
        .await?;

        // Determine step result
        let exit_code = output.status.code().unwrap_or(1);
        let status = if output.status.success() {
            "success"
        } else {
            "failed"
        };
        let error_message = if output.status.success() {
            String::new()
        } else {
            format!("Command failed with exit code {}", exit_code)
        };

        if !output.status.success() {
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
                exit_code: exit_code as i32,
                status: status.to_string(),
                error_message,
                ts_unix_ms: current_timestamp_ms(),
            })),
        })
        .await?;

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

    // Close the sender to signal completion
    drop(tx);

    // Wait for response
    // let response = response_future.await?;
    let _response = response_future.await?; 
    println!("📊 Job {} completed with status: {}", job_id, job_status);

    Ok(())
}

fn current_timestamp_ms() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_millis() as i64
}