import { RouterProvider } from 'react-router';
import { Toaster } from 'sonner';
import { router } from './routes';
import { ThemeProvider } from './components/ThemeProvider';
import { AppErrorBoundary } from './components/ErrorBoundary';

export default function App() {
  return (
    <AppErrorBoundary>
      <ThemeProvider>
        <RouterProvider router={router} />
        <Toaster theme="system" position="bottom-right" richColors closeButton />
      </ThemeProvider>
    </AppErrorBoundary>
  );
}
