import React, { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import axios from 'axios';
import { formatDistanceToNow } from 'date-fns';

// const API_URL = 'http://localhost:8080';
const API_URL = process.env.REACT_APP_API_URL || 'http://localhost/api';

function RunsList() {
  const [runs, setRuns] = useState([]);
  const [loading, setLoading] = useState(true);
  const [triggerForm, setTriggerForm] = useState({
    repo_path: 'control-plane/examples/repo',
    workflow_path: '.ci/workflows/build.yml'
  });

  useEffect(() => {
    fetchRuns();
    const interval = setInterval(fetchRuns, 3000); // Refresh every 3s
    return () => clearInterval(interval);
  }, []);

  const fetchRuns = async () => {
    try {
      const response = await axios.get(`${API_URL}/runs`);
      setRuns(response.data || []);
      setLoading(false);
    } catch (error) {
      console.error('Failed to fetch runs:', error);
      setLoading(false);
    }
  };

  const triggerWorkflow = async (e) => {
    e.preventDefault();
    try {
      await axios.post(`${API_URL}/runs/trigger`, triggerForm);
      fetchRuns();
    } catch (error) {
      alert('Failed to trigger workflow: ' + error.message);
    }
  };

  const getStatusColor = (status) => {
    const colors = {
      success: '#28a745',
      failed: '#dc3545',
      running: '#007bff',
      queued: '#ffc107',
      pending: '#6c757d',
      skipped: '#17a2b8'
    };
    return colors[status] || '#6c757d';
  };

  if (loading) return <div className="loading">Loading...</div>;

  return (
    <div className="runs-list">
      <div className="header">
        <h1>Workflow Runs</h1>
        <button 
          onClick={() => document.getElementById('trigger-form').style.display = 'block'}
          className="btn btn-primary"
        >
          + Trigger Workflow
        </button>
      </div>

      <div id="trigger-form" style={{display: 'none'}} className="trigger-form">
        <h3>Trigger New Workflow</h3>
        <form onSubmit={triggerWorkflow}>
          <input
            type="text"
            placeholder="Repo Path"
            value={triggerForm.repo_path}
            onChange={(e) => setTriggerForm({...triggerForm, repo_path: e.target.value})}
          />
          <input
            type="text"
            placeholder="Workflow Path"
            value={triggerForm.workflow_path}
            onChange={(e) => setTriggerForm({...triggerForm, workflow_path: e.target.value})}
          />
          <button type="submit" className="btn btn-primary">Trigger</button>
          <button 
            type="button" 
            onClick={() => document.getElementById('trigger-form').style.display = 'none'}
            className="btn btn-secondary"
          >
            Cancel
          </button>
        </form>
      </div>

      <div className="runs-grid">
        {runs.map(run => (
          <Link to={`/runs/${run.ID}`} key={run.ID} className="run-card">
            <div className="run-header">
              <span className="run-id">#{run.ID}</span>
              <span 
                className="status-badge"
                style={{backgroundColor: getStatusColor(run.Status)}}
              >
                {run.Status}
              </span>
            </div>
            <div className="run-body">
              <div className="run-repo">{run.Repo}</div>
              <div className="run-ref">{run.Ref}</div>
              <div className="run-time">
                {formatDistanceToNow(new Date(run.CreatedAt), { addSuffix: true })}
              </div>
            </div>
          </Link>
        ))}
      </div>
    </div>
  );
}

export default RunsList;