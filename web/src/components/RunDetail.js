import React, { useState, useEffect, useCallback } from 'react';
import { useParams, Link } from 'react-router-dom';
import axios from 'axios';

const API_URL = 'http://localhost:8080';

function RunDetail() {
  const { runId } = useParams();
  const [jobs, setJobs] = useState([]);
  const [loading, setLoading] = useState(true);

  const fetchJobs = useCallback(async () => {
    try {
      const response = await axios.get(`${API_URL}/runs/${runId}/jobs`);
      setJobs(response.data || []);
      setLoading(false);
    } catch (error) {
      console.error('Failed to fetch jobs:', error);
      setLoading(false);
    }
  }, [runId]);


  useEffect(() => {
    fetchJobs();
    const interval = setInterval(fetchJobs, 2000);
    return () => clearInterval(interval);
  }, [fetchJobs]);


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

  const rerunAllJobs = async () => {
    if (!window.confirm('Re-run all jobs in this run?')) {
      return;
    }
    
    try {
      await axios.post(`${API_URL}/runs/${runId}/rerun`);
      alert('All jobs requeued successfully!');
      window.location.reload();
    } catch (error) {
      alert('Failed to rerun: ' + error.message);
    }
  };


  if (loading) return <div className="loading">Loading...</div>;

  return (
    <div className="run-detail">
      <div className="breadcrumb">
        <Link to="/">Runs</Link> / Run #{runId}
        <button onClick={rerunAllJobs} className="btn btn-primary">
          🔄 Re-run All Jobs
        </button>
      </div>
      
        {/* ✅ RE-RUN ALL BUTTON HERE */}
      {/* <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '2rem'}}>
        <h1>Run #{runId}</h1>
        <button onClick={rerunAllJobs} className="btn btn-primary">
          🔄 Re-run All Jobs
        </button>
      </div> */}
      
      
      <div className="jobs-list">
        {jobs.map(job => (
          <Link to={`/jobs/${job.ID}`} key={job.ID} className="job-card">
            <div className="job-icon">{getStatusIcon(job.Status)}</div>
            <div className="job-info">
              <div className="job-header">
                <span className="job-name">{job.Name}</span>
                <span className="job-id">#{job.ID}</span>
              </div>
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