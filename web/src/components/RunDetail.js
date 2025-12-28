import React, { useState, useEffect } from 'react';
import { useParams, Link } from 'react-router-dom';
import axios from 'axios';

const API_URL = 'http://localhost:8080';

function RunDetail() {
  const { runId } = useParams();
  const [jobs, setJobs] = useState([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetchJobs();
    const interval = setInterval(fetchJobs, 2000);
    return () => clearInterval(interval);
  }, [runId]);

  const fetchJobs = async () => {
    try {
      const response = await axios.get(`${API_URL}/runs/${runId}/jobs`);
      setJobs(response.data || []);
      setLoading(false);
    } catch (error) {
      console.error('Failed to fetch jobs:', error);
      setLoading(false);
    }
  };

  const getStatusIcon = (status) => {
    const icons = {
      success: '✅',
      failed: '❌',
      running: '🔄',
      queued: '⏳',
      pending: '⏸️',
      skipped: '⏭️'
    };
    return icons[status] || '❓';
  };

  if (loading) return <div className="loading">Loading...</div>;

  return (
    <div className="run-detail">
      <div className="breadcrumb">
        <Link to="/">Runs</Link> / Run #{runId}
      </div>
      
      <h1>Run #{runId}</h1>
      
      <div className="jobs-list">
        {jobs.map(job => (
          <Link to={`/jobs/${job.ID}`} key={job.ID} className="job-card">
            <div className="job-icon">{getStatusIcon(job.Status)}</div>
            <div className="job-info">
              <div className="job-name">{job.Name}</div>
              <div className="job-status">{job.Status}</div>
              {job.Needs && job.Needs.length > 0 && (
                <div className="job-needs">
                  Needs: {job.Needs.join(', ')}
                </div>
              )}
            </div>
            <div className="job-meta">
              {job.RunnerID && <div>Runner: {job.RunnerID}</div>}
              {job.StartedAt && (
                <div>Duration: {calculateDuration(job.StartedAt, job.FinishedAt)}</div>
              )}
            </div>
          </Link>
        ))}
      </div>
    </div>
  );
}

function calculateDuration(start, end) {
  if (!start) return 'Not started';
  if (!end) return 'Running...';
  const ms = new Date(end) - new Date(start);
  return `${(ms / 1000).toFixed(1)}s`;
}

export default RunDetail;