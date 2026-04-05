import { Download } from 'lucide-react';
import { Button } from './ui/button';

interface ExportButtonProps {
  data: any;
  filename: string;
  format?: 'json' | 'csv';
  label?: string;
}

function sanitizeCsvCell(str: string): string {
  // Neutralize spreadsheet formula injection: prefix dangerous leading chars.
  // The tab-prefix approach ensures the value is always treated as text, even
  // when wrapped in CSV double-quotes (a leading ' inside quotes is NOT enough
  // to suppress formula evaluation in Excel/LibreOffice).
  const needsPrefix = /^[=+\-@\t\r]/.test(str);
  if (needsPrefix) {
    str = '\t' + str;
  }
  if (str.includes(',') || str.includes('"') || str.includes('\n') || str.includes('\t')) {
    return `"${str.replace(/"/g, '""')}"`;
  }
  return str;
}

function toCsv(data: any[]): string {
  if (data.length === 0) return '';
  const headers = Object.keys(data[0]);
  const rows = data.map((row) =>
    headers.map((h) => {
      const val = row[h] ?? '';
      const str = typeof val === 'object' ? JSON.stringify(val) : String(val);
      return sanitizeCsvCell(str);
    }).join(',')
  );
  return [headers.join(','), ...rows].join('\n');
}

export function ExportButton({ data, filename, format = 'json', label }: ExportButtonProps) {
  const handleExport = () => {
    const raw = format === 'csv' && Array.isArray(data) ? toCsv(data) : JSON.stringify(data, null, 2);
    const mime = format === 'csv' ? 'text/csv;charset=utf-8' : 'application/json';
    // BOM prefix for CSV ensures Excel on Windows handles UTF-8 correctly
    const content = format === 'csv' ? '\uFEFF' + raw : raw;
    const blob = new Blob([content], { type: mime });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${filename}.${format}`;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <Button variant="outline" size="sm" onClick={handleExport} disabled={!data || (Array.isArray(data) && data.length === 0)}>
      <Download className="w-3 h-3 mr-1" />
      {label || `Export ${format.toUpperCase()}`}
    </Button>
  );
}
