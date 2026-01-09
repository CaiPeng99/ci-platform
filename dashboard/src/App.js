import React from 'react';
import { BrowserRouter, Routes, Route, Link } from 'react-router-dom';
import RunsList from './components/RunsList';
import RunDetail from './components/RunDetail';
import JobDetail from './components/JobDetail';
import './App.css';

function App() {
  return (
    <BrowserRouter>
      <div className="App">
        <nav className="navbar">
          <div className="container">
            <Link to="/" className="logo">
              🚀 CI/CD Platform
            </Link>
            <div className="nav-links">
              <Link to="/">Runs</Link>
            </div>
          </div>
        </nav>
        
        <div className="container">
          <Routes>
            <Route path="/" element={<RunsList />} />
            <Route path="/runs/:runId" element={<RunDetail />} />
            <Route path="/jobs/:jobId" element={<JobDetail />} />
          </Routes>
        </div>
      </div>
    </BrowserRouter>
  );
}

export default App;