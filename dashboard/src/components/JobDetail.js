// import React, { useState, useEffect, useCallback } from 'react';
// import { useParams, Link } from 'react-router-dom';
// import axios from 'axios';

// const API_URL = 'http://localhost:8080';

// function JobDetail() {
//   const { jobId } = useParams();
//   const [job, setJob] = useState(null);
//   const [logs, setLogs] = useState([]);
//   const [artifacts, setArtifacts] = useState([]);
//   // const [streaming, setStreaming] = useState(false);
//   const eventSourceRef = useRef(null);


//   const fetchJob = useCallback(async () => {
//     try {
//       const response = await axios.get(`${API_URL}/jobs/${jobId}`);
//       setJob(response.data);
//     } catch (error) {
//       console.error('Failed to fetch job:', error);
//     }
//   }, [jobId]);


//   const fetchArtifacts = useCallback(async () => {
//     try {
//       const response = await axios.get(`${API_URL}/jobs/${jobId}/artifacts`);
//       setArtifacts(response.data || []);
//     } catch (error) {
//       console.error('Failed to fetch artifacts:', error);
//     }
//   }, [jobId]);

//   const streamLogs =  useCallback(() => {
//     if (streaming) return;
//     setStreaming(true);

//     const eventSource = new EventSource(`${API_URL}/jobs/${jobId}/logs/stream`);
    
//     eventSource.addEventListener('log_chunk', (e) => {
//       setLogs(prev => [...prev, e.data]);
//     });

//     eventSource.addEventListener('ping', (e) => {
//       // Keep-alive
//     });

//     eventSource.onerror = () => {
//       eventSource.close();
//       setStreaming(false);
//     };

//     return () => eventSource.close();
//   }, [jobId, streaming]);

//     useEffect(() => {
//     fetchJob();
//     fetchArtifacts();
//     const cleanup = streamLogs();
//     return cleanup;
//   }, [fetchJob, fetchArtifacts, streamLogs]); 
  

//   const downloadArtifact = async(artifactId, name) => {
//     // window.open(`${API_URL}/artifacts/${artifactId}`, '_blank');
//     try{
//       const response = await axios.get(`${API_URL}/artifacts/${artifactId}/download`);
//       const downloadUrl = response.data.download_url;

//       // Open in new tab to download
//       window.open(downloadUrl, '_blank');
//     } catch(error){
//       alert('Failed to download artifact: ' + error.message);
//     }
//   };

//   const formatBytes = (bytes) => {
//     if (bytes === 0) return '0 Bytes';
//     const k = 1024;
//     const sizes = ['Bytes', 'KB', 'MB', 'GB'];
//     const i = Math.floor(Math.log(bytes) / Math.log(k));
//     return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
//   };

//   const rerunJob = async () => {
//     if (!window.confirm('Are you sure you want to re-run this job?')) {
//       return;
//     }
    
//     try {
//       await axios.post(`${API_URL}/jobs/${jobId}/rerun`);
//       alert('Job requeued successfully!');
//       window.location.reload();
//     } catch (error) {
//       alert('Failed to rerun job: ' + error.message);
//     }
//   };


//   if (!job) return <div className="loading">Loading...</div>;

//   return (
//     <div className="job-detail">
//       <div className="breadcrumb">
//         <Link to="/">Runs</Link> / 
//         <Link to={`/runs/${job.RunID}`}>Run #{job.RunID}</Link> / 
//         Job #{jobId}
//       </div>

//        <div className="job-header">
//         <h1>{job.Name} #{job.ID}</h1>
//           <div style={{display: 'flex', gap: '1rem', alignItems: 'center'}}>
//             <span className={`status-badge status-${job.Status}`}>
//               {job.Status}
//             </span>
//           </div>
//       </div>

//       <div className="job-info-grid">
//         <div className="info-item">
//           <label>Runner:</label>
//           <span>{job.RunnerID || 'Not assigned'}</span>
//         </div>
//         <div className="info-item">
//           <label>Attempts:</label>
//           <span>{job.Attempts} / {job.MaxAttempts}</span>
//         </div>
//         {job.StartedAt && (
//           <div className="info-item">
//             <label>Started:</label>
//             <span>{new Date(job.StartedAt).toLocaleString()}</span>
//           </div>
//         )}
//         {job.FinishedAt && job.FinishedAt !== '0001-01-01T00:00:00Z' && (
//           <div className="info-item">
//             <label>Finished:</label>
//             <span>{new Date(job.FinishedAt).toLocaleString()}</span>
//           </div>
//         )}
//         {job.StartedAt && job.FinishedAt && 
//          job.StartedAt !== '0001-01-01T00:00:00Z' && 
//          job.FinishedAt !== '0001-01-01T00:00:00Z' && (
//           <div className="info-item">
//             <label>Duration:</label>
//             <span>{calculateDuration(job.StartedAt, job.FinishedAt)}</span>
//           </div>
//         )}
//       </div>

//       {artifacts.length > 0 && (
//         <div className="artifacts-section">
//           <h2>📦 Artifacts ({artifacts.length})</h2>
//           <div className="artifacts-list">
//             {artifacts.map(artifact => (
//               <div key={artifact.ID} className="artifact-item">
//                  <div className="artifact-info">
//                   <span className="artifact-icon">📄</span>
//                   <span className="artifact-name">{artifact.Name}</span>
//                 </div>
//                 <div className="artifact-meta">
//                   <span className="artifact-size">{formatBytes(artifact.SizeBytes)}</span>
//                   <button 
//                     onClick={() => downloadArtifact(artifact.ID, artifact.Name)}
//                     className="btn btn-sm btn-primary"
//                   >
//                   ⬇ Download
//                 </button>
//               </div>
//             </div>  
//           ))}
//         </div>
//       </div>
//     )}

//       <div className="logs-section">
//         <h2>📝 Logs</h2>
//         <div className="logs-container">
//           {logs.map((log, i) => (
//             <div key={i} className="log-line">{log}</div>
//           ))}
//           {logs.length === 0 && <div className="no-logs">No logs yet...</div>}
//         </div>
//       </div>
//     </div>
//   );
// }

// function calculateDuration(start, end) {
//   if (!start || !end) return 'N/A';
//   const ms = new Date(end) - new Date(start);
//   if (ms < 0) return 'N/A';
//   return `${(ms / 1000).toFixed(1)}s`;
// }

// export default JobDetail;


import React, { useState, useEffect, useRef } from 'react';
import { useParams, Link } from 'react-router-dom';
import axios from 'axios';

const API_URL = 'http://localhost:8080';

function JobDetail() {
  const { jobId } = useParams();
  const [job, setJob] = useState(null);
  const [logs, setLogs] = useState([]);
  const [artifacts, setArtifacts] = useState([]);
  const eventSourceRef = useRef(null);

  useEffect(() => {
    fetchJob();
    fetchArtifacts();
    
    // Set up SSE connection
    console.log('🔌 Connecting to SSE for job:', jobId);
    const url = `${API_URL}/jobs/${jobId}/logs/stream`;
    const eventSource = new EventSource(url);
    eventSourceRef.current = eventSource;
    
    eventSource.onopen = () => {
      console.log('✅ SSE connection opened');
    };
    
    eventSource.addEventListener('log_chunk', (e) => {
      console.log('📝 Log received:', e.data.substring(0, 50));
      setLogs(prev => [...prev, e.data]);
    });

    eventSource.addEventListener('ping', (e) => {
      console.log('🏓 Ping');
    });

    eventSource.onerror = (error) => {
      console.error('❌ SSE error:', error);
      console.log('ReadyState:', eventSource.readyState);
    };
    
    // Artifact polling
    const artifactInterval = setInterval(fetchArtifacts, 5000);
    
    // Cleanup
    return () => {
      console.log('🔌 Closing SSE connection');
      clearInterval(artifactInterval);
      eventSource.close();
    };
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

  const downloadArtifact = async (artifactId, name) => {
    try {
      const response = await axios.get(`${API_URL}/artifacts/${artifactId}/download`);
      const downloadUrl = response.data.download_url;
      window.open(downloadUrl, '_blank');
    } catch (error) {
      alert('Failed to download artifact: ' + error.message);
    }
  };

  const formatBytes = (bytes) => {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
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
        <h1>{job.Name} <span style={{color: '#666', fontSize: '1.5rem'}}>#{job.ID}</span></h1>
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
        {job.StartedAt && job.StartedAt !== '0001-01-01T00:00:00Z' && (
          <div className="info-item">
            <label>Started:</label>
            <span>{new Date(job.StartedAt).toLocaleString()}</span>
          </div>
        )}
        {job.FinishedAt && job.FinishedAt !== '0001-01-01T00:00:00Z' && (
          <div className="info-item">
            <label>Finished:</label>
            <span>{new Date(job.FinishedAt).toLocaleString()}</span>
          </div>
        )}
        {job.StartedAt && job.FinishedAt && 
         job.StartedAt !== '0001-01-01T00:00:00Z' && 
         job.FinishedAt !== '0001-01-01T00:00:00Z' && (
          <div className="info-item">
            <label>Duration:</label>
            <span>{calculateDuration(job.StartedAt, job.FinishedAt)}</span>
          </div>
        )}
      </div>

      {artifacts.length > 0 && (
        <div className="artifacts-section">
          <h2>📦 Artifacts ({artifacts.length})</h2>
          <div className="artifacts-list">
            {artifacts.map(artifact => (
              <div key={artifact.ID} className="artifact-item">
                <div className="artifact-info">
                  <span className="artifact-icon">📄</span>
                  <span className="artifact-name">{artifact.Name}</span>
                </div>
                <div className="artifact-meta">
                  <span className="artifact-size">{formatBytes(artifact.SizeBytes)}</span>
                  <button 
                    onClick={() => downloadArtifact(artifact.ID, artifact.Name)}
                    className="btn btn-sm btn-primary"
                  >
                    ⬇ Download
                  </button>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="logs-section">
        <h2>📝 Logs</h2>
        <div className="logs-container">
          {logs.length === 0 ? (
            <div className="no-logs">No logs yet...</div>
          ) : (
            logs.map((log, i) => (
              <div key={i} className="log-line">{log}</div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}

function calculateDuration(start, end) {
  if (!start || !end) return 'N/A';
  const ms = new Date(end) - new Date(start);
  if (ms < 0) return 'N/A';
  return `${(ms / 1000).toFixed(1)}s`;
}

export default JobDetail;