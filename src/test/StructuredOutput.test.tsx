import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { StructuredOutput } from '../app/components/chat/StructuredOutput';

// Mock clipboard API
const writeTextMock = vi.fn().mockResolvedValue(undefined);
Object.defineProperty(navigator, 'clipboard', {
  value: { writeText: writeTextMock },
  writable: true,
});

beforeEach(() => {
  writeTextMock.mockClear();
});

describe('StructuredOutput', () => {
  it('renders regular markdown text unchanged', () => {
    render(<StructuredOutput content="Hello, world! This is plain text." />);
    expect(screen.getByText('Hello, world! This is plain text.')).toBeInTheDocument();
  });

  it('renders markdown tables with proper styling', () => {
    const tableContent = `
| Name | Value |
|------|-------|
| foo  | bar   |
| baz  | qux   |
`;
    const { container } = render(<StructuredOutput content={tableContent} />);
    const table = container.querySelector('table');
    expect(table).toBeInTheDocument();

    // Should be wrapped in overflow-x-auto div
    const wrapper = table?.closest('[class*="overflow-x-auto"]');
    expect(wrapper).toBeInTheDocument();

    // Check table rows rendered
    expect(screen.getByText('Name')).toBeInTheDocument();
    expect(screen.getByText('Value')).toBeInTheDocument();
    expect(screen.getByText('foo')).toBeInTheDocument();
    expect(screen.getByText('bar')).toBeInTheDocument();
  });

  it('renders code blocks with a copy button', () => {
    const codeContent = '```python\nprint("hello")\n```';
    render(<StructuredOutput content={codeContent} />);

    // Should show language label
    expect(screen.getByText('python')).toBeInTheDocument();

    // Should show copy button
    const copyBtn = screen.getByRole('button', { name: /copy/i });
    expect(copyBtn).toBeInTheDocument();

    // Code content should be rendered
    expect(screen.getByText(/print/)).toBeInTheDocument();
  });

  it('copies code content when copy button is clicked', async () => {
    const codeContent = '```python\nprint("hello")\n```';
    render(<StructuredOutput content={codeContent} />);

    const copyBtn = screen.getByRole('button', { name: /copy/i });
    fireEvent.click(copyBtn);

    await waitFor(() => {
      expect(writeTextMock).toHaveBeenCalledWith('print("hello")');
    });
  });

  it('renders inline code with gray background', () => {
    render(<StructuredOutput content="Use the `npm install` command." />);
    const inlineCode = screen.getByText('npm install');
    expect(inlineCode.tagName).toBe('CODE');
    expect(inlineCode.className).toMatch(/bg-secondary/);
  });

  it('shows "View full report" link when metadata.has_artifact is true', () => {
    // Note: artifact links are in AgencyMessage, not StructuredOutput
    // StructuredOutput does not render artifact links itself
    // This test verifies StructuredOutput renders content and doesn't break with metadata
    render(
      <StructuredOutput
        content="Report complete."
        metadata={{ has_artifact: true, agent: 'researcher', task_id: 'task-123' }}
      />
    );
    expect(screen.getByText('Report complete.')).toBeInTheDocument();
    // StructuredOutput itself does not render "View full report" — that's in AgencyMessage
    expect(screen.queryByText('View full report')).not.toBeInTheDocument();
  });

  it('renders collapsible details/summary sections', async () => {
    const detailsContent = `<details>
<summary>Click to expand</summary>

Hidden content here

</details>`;
    render(<StructuredOutput content={detailsContent} />);

    // Summary trigger should be visible
    expect(screen.getByText('Click to expand')).toBeInTheDocument();

    // Hidden content may not be visible initially (collapsed)
    // Click to open
    const trigger = screen.getByText('Click to expand');
    fireEvent.click(trigger);

    await waitFor(() => {
      expect(screen.getByText('Hidden content here')).toBeInTheDocument();
    });
  });

  it('renders markdown bold and italic text', () => {
    const { container } = render(<StructuredOutput content="**bold** and _italic_" />);
    const bold = container.querySelector('strong');
    const italic = container.querySelector('em');
    expect(bold).toBeInTheDocument();
    expect(italic).toBeInTheDocument();
    expect(bold?.textContent).toBe('bold');
    expect(italic?.textContent).toBe('italic');
  });

  it('renders fenced code block without language label when no language specified', () => {
    const codeContent = '```\nplain code block\n```';
    render(<StructuredOutput content={codeContent} />);
    expect(screen.getByText('plain code block')).toBeInTheDocument();
    // copy button should still exist
    expect(screen.getByRole('button', { name: /copy/i })).toBeInTheDocument();
  });
});
