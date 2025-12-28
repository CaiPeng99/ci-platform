import React, { useState, useEffect } from 'react';
import { useParams, Link } from 'react-router-dom';
import axios from 'axios';

const API_URL = 'http://localhost:8080';

function JobDetail() {
  const { jobId } = useParams();
  const [job, setJob] = useState(null);
  const [logs, setLogs] = useState([]);
  const [artifacts, setArtifacts] = useState([]);
  const [streaming, setStreaming] = useState(false);

  useEffect(() => {
    fetchJob();
    fetchArtifacts();
    streamLogs();
  }, [jobId]);

  const fetchJob = async () => {
    try {
      const response = await axios.get(`${API_URL}/jobs/${jobId}`);
      setJob(response.data);
    } catch (error) {
      console.error('Failed to fetch job:', error);
    }
  };

  const fetchArtifacts = async () => {
    try {
      const response = await axios.get(`${API_URL}/jobs/${jobId}/artifacts`);
      setArtifacts(response.data || []);
    } catch (error) {
      console.error('Failed to fetch artifacts:', error);
    }
  };

  const streamLogs = () => {
    if (streaming) return;
    setStreaming(true);

    const eventSource = new EventSource(`${API_URL}/jobs/${jobId}/logs/stream`);
    
    eventSource.addEventListener('log_chunk', (e) => {
      setLogs(prev => [...prev, e.data]);
    });

    eventSource.addEventListener('ping', (e) => {
      // Keep-alive
    });

    eventSource.onerror = () => {
      eventSource.close();
      setStreaming(false);
    };

    return () => eventSource.close();
  };

  const downloadArtifact = (artifactId, name) => {
    window.open(`${API_URL}/artifacts/${artifactId}`, '_blank');
  };

  if (!job) return <div className="loading">Loading...</div>;

  return (
    <div className="job-detail">
      <div className="breadcrumb">
        <Link to="/">Runs</Link> / 
        <Link to={`/runs/${job.RunID}`}>Run #{job.RunID}</Link> / 
        Job #{jobId}
      </div>

      <div className="job-header">
        <h1>{job.Name}</h1>
        <span className={`status-badge status-${job.Status}`}>
          {job.Status}
        </span>
      </div>

      <div className="job-info-grid">
        <div className="info-item">
          <label>Runner:</label>
          <span>{job.RunnerID || 'Not assigned'}</span>
        </div>
        <div className="info-item">
          <label>Attempts:</label>
          <span>{job.Attempts} / {job.MaxAttempts}</span>
        </div>
        {job.StartedAt && (
          <div className="info-item">
            <label>Started:</label>
            <span>{new Date(job.StartedAt).toLocaleString()}</span>
          </div>
        )}
        {job.FinishedAt && (
          <div className="info-item">
            <label>Finished:</label>
            <span>{new Date(job.FinishedAt).toLocaleString()}</span>
          </div>
        )}
      </div>

      {artifacts.length > 0 && (
        <div className="artifacts-section">
          <h2>📦 Artifacts</h2>
          <div className="artifacts-list">
            {artifacts.map(artifact => (
              <div key={artifact.ID} className="artifact-item">
                <span className="artifact-name">{artifact.Name}</span>
                <span className="artifact-size">
                  {(artifact.SizeBytes / 1024).toFixed(2)} KB
                </span>
                <button 
                  onClick={() => downloadArtifact(artifact.ID, artifact.Name)}
                  className="btn btn-sm"
                >
                  Download
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="logs-section">
        <h2>📝 Logs</h2>
        <div className="logs-container">
          {logs.map((log, i) => (
            <div key={i} className="log-line">{log}</div>
          ))}
          {logs.length === 0 && <div className="no-logs">No logs yet...</div>}
        </div>
      </div>
    </div>
  );
}

export default JobDetail;