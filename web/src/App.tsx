import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { Layout } from '@/components/layout/Layout';
import { HomePage } from '@/pages/HomePage';
import { ResultsPage } from '@/pages/ResultsPage';
import { PoetryPage } from '@/pages/PoetryPage';

export default function App() {
  return (
    <BrowserRouter>
      <Layout>
        <Routes>
          <Route path="/" element={<HomePage />} />
          <Route path="/results/:taskId" element={<ResultsPage />} />
          <Route path="/poetry" element={<PoetryPage />} />
        </Routes>
      </Layout>
    </BrowserRouter>
  );
}