import { useCallback, useRef, useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router';
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  addEdge,
  useNodesState,
  useEdgesState,
  type Connection,
  type OnConnect,
  ReactFlowProvider,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { toast } from 'sonner';

import { composerNodeTypes } from './nodeTypes';
import { NodePalette } from './NodePalette';
import { PropertyPanel } from './PropertyPanel';
import { ComposerToolbar } from './ComposerToolbar';
import { isValidConnection, validateCanvas } from './canvasValidator';
import { toCanvasDocument, fromCanvasDocument, canvasToWizardState } from './canvasSerializer';
import { serializeToYaml } from '../serialize';
import { api } from '@/app/lib/api';
import type { CanvasNode, CanvasEdge } from './canvasTypes';
import { getNodeDef } from './nodeRegistry';

function ComposerInner() {
  const { name } = useParams<{ name: string }>();
  const navigate = useNavigate();
  const reactFlowWrapper = useRef<HTMLDivElement>(null);
  const [nodes, setNodes, onNodesChange] = useNodesState<CanvasNode>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<CanvasEdge>([]);
  const [selectedNode, setSelectedNode] = useState<CanvasNode | null>(null);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [reactFlowInstance, setReactFlowInstance] = useState<any>(null);

  // Load existing canvas
  useEffect(() => {
    if (!name) return;
    api.missions.canvas(name).then(doc => {
      if (doc) {
        const { nodes: n, edges: e } = fromCanvasDocument(doc);
        setNodes(n);
        setEdges(e);
      }
    }).catch(() => {
      // No canvas yet — that's fine
    });
  }, [name]);

  const onConnect: OnConnect = useCallback((connection: Connection) => {
    const sourceNode = nodes.find((n: CanvasNode) => n.id === connection.source);
    const targetNode = nodes.find((n: CanvasNode) => n.id === connection.target);
    if (!isValidConnection(sourceNode?.type, (connection.sourceHandle as string | undefined) ?? undefined, targetNode?.type, (connection.targetHandle as string | undefined) ?? undefined)) {
      toast.error('Invalid connection — port types must match');
      return;
    }
    setEdges(eds => addEdge(connection, eds));
    setDirty(true);
  }, [nodes, setEdges]);

  const onDragOver = useCallback((event: React.DragEvent) => {
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
  }, []);

  const onDrop = useCallback((event: React.DragEvent) => {
    event.preventDefault();
    const typeId = event.dataTransfer.getData('application/reactflow-type');
    if (!typeId || !reactFlowInstance || !reactFlowWrapper.current) return;

    const def = getNodeDef(typeId);
    if (!def) return;

    // Only allow one agent node
    if (def.category === 'agent' && nodes.some(n => n.type === 'agent')) {
      toast.error('Canvas can only have one Agent node');
      return;
    }

    const bounds = reactFlowWrapper.current.getBoundingClientRect();
    const position = reactFlowInstance.screenToFlowPosition({
      x: event.clientX - bounds.left,
      y: event.clientY - bounds.top,
    });

    const newNode: CanvasNode = {
      id: `n-${crypto.randomUUID().slice(0, 8)}`,
      type: typeId,
      position,
      data: { typeId, config: {} },
    };

    setNodes(nds => [...nds, newNode]);
    setDirty(true);
  }, [reactFlowInstance, nodes, setNodes]);

  const onNodeClick = useCallback((_: React.MouseEvent, node: CanvasNode) => {
    setSelectedNode(node);
  }, []);

  const onPaneClick = useCallback(() => {
    setSelectedNode(null);
  }, []);

  const handleConfigChange = useCallback((nodeId: string, config: Record<string, unknown>) => {
    setNodes(nds => nds.map(n =>
      n.id === nodeId ? { ...n, data: { ...n.data, config } } : n
    ));
    setSelectedNode(prev => prev?.id === nodeId ? { ...prev, data: { ...prev.data, config } } as CanvasNode : prev);
    setDirty(true);
  }, [setNodes]);

  const handleSave = useCallback(async () => {
    if (!name) return;
    setSaving(true);
    try {
      const doc = toCanvasDocument(nodes, edges);
      await api.missions.saveCanvas(name, doc);
      setDirty(false);
      toast.success('Canvas saved');
    } catch (err) {
      toast.error(`Save failed: ${err}`);
    } finally {
      setSaving(false);
    }
  }, [name, nodes, edges]);

  const handleValidate = useCallback(() => {
    const errors = validateCanvas(nodes as CanvasNode[], edges);
    if (errors.length === 0) {
      toast.success('Canvas is valid');
    } else {
      errors.forEach(e => toast.error(e.message));
    }
    return errors;
  }, [nodes, edges]);

  const handleDeploy = useCallback(async () => {
    const errors = validateCanvas(nodes as CanvasNode[], edges);
    if (errors.length > 0) {
      errors.forEach(e => toast.error(e.message));
      return;
    }

    try {
      const doc = toCanvasDocument(nodes, edges);
      const state = canvasToWizardState(doc);
      const yaml = serializeToYaml(state);

      // Save canvas + generate mission YAML
      if (name) {
        await api.missions.saveCanvas(name, doc);
        await api.missions.update(name, yaml);
        toast.success('Mission deployed');
        navigate(`/missions/${name}`);
      } else {
        await api.missions.create(yaml);
        const missionName = state.name;
        await api.missions.saveCanvas(missionName, doc);
        toast.success('Mission created');
        navigate(`/missions/${missionName}`);
      }
    } catch (err) {
      toast.error(`Deploy failed: ${err}`);
    }
  }, [name, nodes, edges, navigate]);

  return (
    <div className="h-screen flex flex-col">
      <ComposerToolbar
        missionName={name || ''}
        onSave={handleSave}
        onValidate={handleValidate}
        onDeploy={handleDeploy}
        saving={saving}
        dirty={dirty}
      />
      <div className="flex flex-1 overflow-hidden">
        <NodePalette />
        <div ref={reactFlowWrapper} className="flex-1">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            onDragOver={onDragOver}
            onDrop={onDrop}
            onNodeClick={onNodeClick}
            onPaneClick={onPaneClick}
            onInit={setReactFlowInstance}
            nodeTypes={composerNodeTypes as any}
            fitView
            snapToGrid
            snapGrid={[16, 16]}
            deleteKeyCode={['Backspace', 'Delete']}
          >
            <Background gap={16} size={1} />
            <Controls />
            <MiniMap
              nodeStrokeWidth={3}
              pannable
              zoomable
            />
          </ReactFlow>
        </div>
        <PropertyPanel node={selectedNode} onChange={handleConfigChange} />
      </div>
    </div>
  );
}

export function MissionComposer() {
  return (
    <ReactFlowProvider>
      <ComposerInner />
    </ReactFlowProvider>
  );
}
