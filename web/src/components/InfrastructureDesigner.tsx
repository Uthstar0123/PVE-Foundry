import React, { useCallback, useMemo, useState } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  Handle,
  Position,
  ConnectionMode,
  applyNodeChanges,
  useStore,
  type Node,
  type Connection,
  type NodeChange,
  type NodeProps,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import {
  Box,
  Copy,
  HardDrive,
  Info,
  Network,
  Play,
  Plus,
  Server,
  Sliders,
  Trash2,
} from 'lucide-react'

export type DesignerNodeKind = 'qemu' | 'lxc' | 'network' | 'storage' | 'template'

export type DesignerNode = {
  id: string
  kind: DesignerNodeKind
  label: string
  x: number
  y: number
  config: Record<string, string | number>
}

export type DesignerEdge = {
  id: string
  source: string
  target: string
}

export type DesignerValue = {
  name: string
  nodes: DesignerNode[]
  edges: DesignerEdge[]
}

export interface InfrastructureDesignerProps {
  value: DesignerValue
  onChange: (value: DesignerValue) => void
  onSubmit: () => void
  busy: boolean
  defaults?: import('@/lib/api').DeploymentDefaults
}

interface PaletteItem {
  kind: DesignerNodeKind
  title: string
  icon: React.ComponentType<{ className?: string }>
  color: string
  defaultLabel: string
  defaultConfig: () => Record<string, string | number>
}

const PALETTE_ITEMS: PaletteItem[] = [
  {
    kind: 'qemu',
    title: 'QEMU VM',
    icon: Server,
    color: 'text-blue-500 bg-blue-500/10 border-blue-500/30',
    defaultLabel: 'ВМ QEMU',
    defaultConfig: () => ({
      vmid: 100,
      nodeName: 'pve',
      poolId: 'production',
      datastoreId: 'local-zfs',
      bridge: 'vmbr0',
      template: 'debian-12-cloudinit',
      cores: 2,
      memory: 4096,
      disk: 32,
    }),
  },
  {
    kind: 'lxc',
    title: 'LXC Контейнер',
    icon: Box,
    color: 'text-emerald-500 bg-emerald-500/10 border-emerald-500/30',
    defaultLabel: 'Контейнер LXC',
    defaultConfig: () => ({
      vmid: 200,
      nodeName: 'pve',
      poolId: 'production',
      datastoreId: 'local-zfs',
      bridge: 'vmbr0',
      template: 'debian-12-standard',
      cores: 1,
      memory: 1024,
      disk: 16,
    }),
  },
  {
    kind: 'network',
    title: 'Сеть',
    icon: Network,
    color: 'text-violet-500 bg-violet-500/10 border-violet-500/30',
    defaultLabel: 'Сетевой мост',
    defaultConfig: () => ({
      bridge: 'vmbr0',
    }),
  },
  {
    kind: 'storage',
    title: 'Хранилище',
    icon: HardDrive,
    color: 'text-amber-500 bg-amber-500/10 border-amber-500/30',
    defaultLabel: 'Хранилище ZFS',
    defaultConfig: () => ({
      datastoreId: 'local-zfs',
    }),
  },
  {
    kind: 'template',
    title: 'Шаблон',
    icon: Copy,
    color: 'text-cyan-500 bg-cyan-500/10 border-cyan-500/30',
    defaultLabel: 'Базовый шаблон',
    defaultConfig: () => ({
      templateId: 'debian-12-cloudinit',
    }),
  },
]

function getKindMeta(kind: DesignerNodeKind) {
  return PALETTE_ITEMS.find((item) => item.kind === kind) ?? PALETTE_ITEMS[0]
}

function beginConnection(event: React.PointerEvent, source: string, connect: (connection: Connection) => void) {
  if (event.button !== 0) return
  event.preventDefault()
  event.stopPropagation()
  window.addEventListener('pointerup', (up) => {
    const target = document.elementFromPoint(up.clientX, up.clientY)?.closest('[data-nodeid]')?.getAttribute('data-nodeid')
    if (target) connect({ source, target, sourceHandle: null, targetHandle: null })
  }, { once: true })
}

function CustomDesignerNode({ data, selected }: NodeProps) {
  const nodeData = data as unknown as DesignerNode & { connect: (connection: Connection) => void }
  const meta = getKindMeta(nodeData.kind)
  const Icon = meta.icon

  return (
    <div
      className={`min-w-[150px] max-w-[200px] rounded-lg border bg-card p-2.5 shadow-sm transition-all ${
        selected
          ? 'border-primary ring-2 ring-primary/40 shadow-md'
          : 'border-border hover:border-primary/50'
      }`}
    >
      <Handle
        type="target"
        position={Position.Left}
        style={{ width: 10, height: 10, visibility: 'visible', zIndex: 20, pointerEvents: 'all' }}
        className="!-left-1.5 !size-2.5 !border-2 !border-background !bg-primary"
      />
      <div className="flex items-center gap-2">
        <span className={`flex size-7 shrink-0 items-center justify-center rounded-md border ${meta.color}`}>
          <Icon className="size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <p className="truncate text-xs font-semibold text-foreground">
            {nodeData.label || meta.defaultLabel}
          </p>
          <p className="text-[10px] text-muted-foreground uppercase tracking-wider">{meta.title}</p>
        </div>
      </div>
      {(nodeData.kind === 'qemu' || nodeData.kind === 'lxc') && (
        <div className="mt-2 flex flex-wrap gap-1 border-t border-border/60 pt-1.5 text-[10px] text-muted-foreground font-mono">
          <span>VMID: {nodeData.config.vmid ?? '—'}</span>
          <span>·</span>
          <span>{nodeData.config.cores ?? '—'}C/{nodeData.config.memory ?? '—'}M</span>
        </div>
      )}
      {nodeData.kind === 'network' && (
        <div className="mt-2 border-t border-border/60 pt-1.5 text-[10px] text-muted-foreground font-mono truncate">
          {String(nodeData.config.bridge ?? 'vmbr0')}
        </div>
      )}
      {nodeData.kind === 'storage' && (
        <div className="mt-2 border-t border-border/60 pt-1.5 text-[10px] text-muted-foreground font-mono truncate">
          {String(nodeData.config.datastoreId ?? 'local-zfs')}
        </div>
      )}
      {nodeData.kind === 'template' && (
        <div className="mt-2 border-t border-border/60 pt-1.5 text-[10px] text-muted-foreground font-mono truncate">
          {String(nodeData.config.templateId ?? 'debian-12')}
        </div>
      )}
      <Handle
        type="source"
        position={Position.Right}
        onPointerDown={(event) => beginConnection(event, nodeData.id, nodeData.connect)}
        style={{ width: 10, height: 10, visibility: 'visible', zIndex: 20, pointerEvents: 'all' }}
        className="!-right-1.5 !size-2.5 !border-2 !border-background !bg-primary"
      />
    </div>
  )
}

function DesignerEdges({ edges }: { edges: DesignerEdge[] }) {
  const nodeLookup = useStore((state) => state.nodeLookup)
  const transform = useStore((state) => state.transform)
  const [offsetX, offsetY, zoom] = transform

  return (
    <svg className="pointer-events-none absolute inset-0 z-[1] size-full overflow-visible" aria-hidden="true">
      {edges.map((edge) => {
        const source = nodeLookup.get(edge.source)
        const target = nodeLookup.get(edge.target)
        if (!source || !target) return null
        const sourcePosition = source.internals.positionAbsolute
        const targetPosition = target.internals.positionAbsolute
        const sourceX = (sourcePosition.x + (source.measured.width ?? 170)) * zoom + offsetX
        const sourceY = (sourcePosition.y + (source.measured.height ?? 80) / 2) * zoom + offsetY
        const targetX = targetPosition.x * zoom + offsetX
        const targetY = (targetPosition.y + (target.measured.height ?? 80) / 2) * zoom + offsetY
        const curve = Math.max(40, Math.abs(targetX - sourceX) / 2)
        const path = `M ${sourceX} ${sourceY} C ${sourceX + curve} ${sourceY}, ${targetX - curve} ${targetY}, ${targetX} ${targetY}`
        return <path key={edge.id} className="react-flow__edge-path" d={path} fill="none" stroke="hsl(var(--primary))" strokeWidth={2} />
      })}
    </svg>
  )
}

const nodeTypes = {
  qemu: CustomDesignerNode,
  lxc: CustomDesignerNode,
  network: CustomDesignerNode,
  storage: CustomDesignerNode,
  template: CustomDesignerNode,
}

export function InfrastructureDesigner({
  value,
  onChange,
  onSubmit,
  busy,
  defaults,
}: InfrastructureDesignerProps) {
  const [selectedId, setSelectedId] = useState<string | null>(null)

  const selectedNode = useMemo(
    () => value.nodes.find((n) => n.id === selectedId) ?? null,
    [value.nodes, selectedId]
  )

  const flowNodes: Node[] = useMemo(() => {
    return value.nodes.map((node) => ({
      id: node.id,
      type: node.kind,
      position: { x: node.x, y: node.y },
      data: { ...node, connect: (connection: Connection) => {
        if (!connection.source || !connection.target || connection.source === connection.target) return
        if (value.edges.some((edge) => edge.source === connection.source && edge.target === connection.target)) return
        onChange({ ...value, edges: [...value.edges, { id: `e-${connection.source}-${connection.target}`, source: connection.source, target: connection.target }] })
      } } as unknown as Record<string, unknown>,
      selected: node.id === selectedId,
      style: { visibility: 'visible' },
    }))
  }, [value, selectedId, onChange])

  const onNodesChange = useCallback(
    (changes: NodeChange[]) => {
      const removedIds = new Set(
        changes.filter((c) => c.type === 'remove').map((c) => (c as { id: string }).id)
      )
      if (removedIds.size > 0) {
        const nextNodes = value.nodes.filter((n) => !removedIds.has(n.id))
        const nextEdges = value.edges.filter(
          (e) => !removedIds.has(e.source) && !removedIds.has(e.target)
        )
        if (selectedId && removedIds.has(selectedId)) {
          setSelectedId(null)
        }
        onChange({ ...value, nodes: nextNodes, edges: nextEdges })
        return
      }

      const updated = applyNodeChanges(changes, flowNodes)
      const posMap = new Map(updated.map((n) => [n.id, n.position]))
      let selectionChanged = false
      let newSelected = selectedId

      for (const change of changes) {
        if (change.type === 'select') {
          selectionChanged = true
          if (change.selected) newSelected = change.id
          else if (newSelected === change.id) newSelected = null
        }
      }

      if (selectionChanged) {
        setSelectedId(newSelected)
      }

      const nextNodes = value.nodes.map((n) => {
        const pos = posMap.get(n.id)
        return pos ? { ...n, x: Math.round(pos.x), y: Math.round(pos.y) } : n
      })

      onChange({ ...value, nodes: nextNodes })
    },
    [flowNodes, value, onChange, selectedId]
  )

  const onConnect = useCallback(
    (connection: Connection) => {
      if (!connection.source || !connection.target || connection.source === connection.target) return
      const exists = value.edges.some(
        (e) => e.source === connection.source && e.target === connection.target
      )
      if (!exists) {
        const newEdge: DesignerEdge = {
          id: `e-${connection.source}-${connection.target}`,
          source: connection.source,
          target: connection.target,
        }
        onChange({ ...value, edges: [...value.edges, newEdge] })
      }
    },
    [value, onChange]
  )

  const addBlock = (item: PaletteItem) => {
    const existingSame = value.nodes.filter((n) => n.kind === item.kind).length
    let suffix = value.nodes.length + 1
    while (value.nodes.some((node) => node.id === `${item.kind}-${suffix}`)) suffix++
    const id = `${item.kind}-${suffix}`
    const offset = existingSame * 24
    const sourceRows = { network: 30, storage: 190, template: 350 }

    const cfg = item.defaultConfig()
    if (defaults) {
      if (item.kind === 'qemu' || item.kind === 'lxc') {
        cfg.nodeName = defaults.nodeName
        cfg.poolId = defaults.poolId
        cfg.datastoreId = defaults.datastoreId
        cfg.bridge = defaults.bridge
      }
      if (item.kind === 'network') cfg.bridge = defaults.bridge
      if (item.kind === 'storage') cfg.datastoreId = defaults.datastoreId
      if (item.kind === 'template') cfg.templateId = defaults.templates[0]?.id ?? ''
    }
    if (item.kind === 'qemu' || item.kind === 'lxc') {
      const highestVmid = value.nodes.reduce((max, n) => {
        const v = typeof n.config.vmid === 'number' ? n.config.vmid : 0
        return v > max ? v : max
      }, (defaults?.nextVmid ?? 100) - 1)
      cfg.vmid = highestVmid + 1
    }

    const newNode: DesignerNode = {
      id,
      kind: item.kind,
      label: item.kind === 'qemu' || item.kind === 'lxc' ? `foundry-${String(cfg.vmid)}` : `${item.defaultLabel} ${existingSame + 1}`,
      x: item.kind === 'qemu' || item.kind === 'lxc' ? 390 + offset : 40 + offset,
      y: item.kind === 'qemu' || item.kind === 'lxc' ? 150 + offset : sourceRows[item.kind] + offset,
      config: cfg,
    }

    onChange({ ...value, nodes: [...value.nodes, newNode] })
    setSelectedId(id)
  }

  const deleteSelectedNode = () => {
    if (!selectedNode) return
    const id = selectedNode.id
    onChange({
      ...value,
      nodes: value.nodes.filter((n) => n.id !== id),
      edges: value.edges.filter((e) => e.source !== id && e.target !== id),
    })
    setSelectedId(null)
  }

  const updateSelectedLabel = (label: string) => {
    if (!selectedNode) return
    onChange({
      ...value,
      nodes: value.nodes.map((n) => (n.id === selectedNode.id ? { ...n, label } : n)),
    })
  }

  const updateSelectedConfig = (key: string, val: string | number) => {
    if (!selectedNode) return
    onChange({
      ...value,
      nodes: value.nodes.map((n) =>
        n.id === selectedNode.id ? { ...n, config: { ...n.config, [key]: val } } : n
      ),
    })
  }

  return (
    <div className="flex w-full max-w-full flex-col gap-4 overflow-x-hidden text-foreground">
      {/* Верхняя панель: имя развёртывания, кнопка запуска Plan */}
      <div className="flex flex-col gap-3 rounded-lg border border-border bg-card p-3 sm:flex-row sm:items-center sm:justify-between">
        <label className="flex min-w-0 flex-1 flex-col gap-1 text-xs sm:max-w-md">
          <span className="font-medium text-muted-foreground">Имя развёртывания</span>
          <input
            value={value.name}
            onChange={(e) => onChange({ ...value, name: e.target.value })}
            placeholder="deployment-production"
            className="h-9 w-full min-w-0 rounded-md border border-border bg-background px-3 font-mono text-sm focus:border-primary focus:outline-none focus:ring-1 focus:ring-primary"
          />
        </label>
        <button
          type="button"
          onClick={onSubmit}
          disabled={busy}
          className="inline-flex h-9 shrink-0 cursor-pointer items-center justify-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
        >
          <Play className="size-4" />
          {busy ? 'Формирование…' : 'Сформировать Plan'}
        </button>
      </div>

      {/* Палитра добавления блоков */}
      <div className="flex flex-col gap-1.5">
        <span className="text-xs font-medium text-muted-foreground">Добавить ресурс в топологию:</span>
        <div className="flex flex-wrap items-center gap-2">
          {PALETTE_ITEMS.map((item) => {
            const Icon = item.icon
            return (
              <button
                key={item.kind}
                type="button"
                onClick={() => addBlock(item)}
                className="inline-flex h-8 cursor-pointer items-center gap-1.5 rounded-md border border-border bg-card px-2.5 text-xs font-medium text-foreground transition-colors hover:border-primary/50 hover:bg-accent active:scale-95"
              >
                <Plus className="size-3.5 text-muted-foreground" />
                <Icon className="size-3.5" />
                <span>{item.title}</span>
              </button>
            )
          })}
        </div>
      </div>

      {/* Рабочая область: канвас React Flow + панель свойств */}
      <div className="grid w-full grid-cols-1 gap-4 lg:grid-cols-[1fr_320px]">
        <div className="relative h-[420px] w-full min-w-0 overflow-hidden rounded-lg border border-border bg-muted/15 sm:h-[500px]">
          <ReactFlow
            key={`${value.nodes.length}-${value.edges.length}`}
            style={{ width: '100%', height: '100%' }}
            nodes={flowNodes}
            onNodesChange={onNodesChange}
            onConnect={onConnect}
            onConnectEnd={(event, state) => {
              const point = 'clientX' in event ? { x: event.clientX, y: event.clientY } : null
              const target = point ? document.elementFromPoint(point.x, point.y)?.closest('[data-nodeid]')?.getAttribute('data-nodeid') : null
              if (state.fromNode?.id && target) onConnect({ source: state.fromNode.id, target, sourceHandle: null, targetHandle: null })
            }}
            onNodeClick={(_e, node) => setSelectedId(node.id)}
            onPaneClick={() => setSelectedId(null)}
            nodeTypes={nodeTypes}
            connectionMode={ConnectionMode.Loose}
            fitView
            minZoom={0.2}
            maxZoom={2}
          >
            <DesignerEdges edges={value.edges} />
            <Background gap={16} size={1} />
            <Controls showInteractive={false} />
          </ReactFlow>
        </div>

        {/* Панель инспектора свойств выбранного узла */}
        <div className="flex w-full flex-col rounded-lg border border-border bg-card p-4">
          <div className="flex items-center justify-between border-b border-border pb-3">
            <div className="flex items-center gap-2">
              <Sliders className="size-4 text-primary" />
              <h3 className="text-sm font-semibold">Свойства блока</h3>
            </div>
            {selectedNode && (
              <button
                type="button"
                onClick={deleteSelectedNode}
                title="Удалить выбранный блок"
                className="inline-flex size-7 cursor-pointer items-center justify-center rounded-md text-destructive transition-colors hover:bg-destructive/10"
              >
                <Trash2 className="size-4" />
              </button>
            )}
          </div>

          {selectedNode ? (
            <div className="mt-3 flex flex-col gap-3 text-xs overflow-y-auto max-h-[420px] pr-0.5">
              <label className="flex flex-col gap-1">
                <span className="text-muted-foreground">Название блока</span>
                <input
                  value={selectedNode.label}
                  onChange={(e) => updateSelectedLabel(e.target.value)}
                  className="h-8 w-full rounded border border-border bg-background px-2 text-xs focus:border-primary focus:outline-none"
                />
              </label>

              {(selectedNode.kind === 'qemu' || selectedNode.kind === 'lxc') && (
                <>
                  <label className="flex flex-col gap-1">
                    <span className="text-muted-foreground">VMID (номер)</span>
                    <input
                      type="number"
                      value={selectedNode.config.vmid ?? ''}
                      onChange={(e) =>
                        updateSelectedConfig('vmid', e.target.value === '' ? '' : Number(e.target.value))
                      }
                      className="h-8 w-full rounded border border-border bg-background px-2 font-mono text-xs focus:border-primary focus:outline-none"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-muted-foreground">Нода Proxmox (nodeName)</span>
                    <input
                      value={String(selectedNode.config.nodeName ?? '')}
                      onChange={(e) => updateSelectedConfig('nodeName', e.target.value)}
                      className="h-8 w-full rounded border border-border bg-background px-2 text-xs focus:border-primary focus:outline-none"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-muted-foreground">Пул ресурсов (poolId)</span>
                    <input
                      value={String(selectedNode.config.poolId ?? '')}
                      onChange={(e) => updateSelectedConfig('poolId', e.target.value)}
                      className="h-8 w-full rounded border border-border bg-background px-2 text-xs focus:border-primary focus:outline-none"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-muted-foreground">Хранилище (datastoreId)</span>
                    <input
                      value={String(selectedNode.config.datastoreId ?? '')}
                      onChange={(e) => updateSelectedConfig('datastoreId', e.target.value)}
                      className="h-8 w-full rounded border border-border bg-background px-2 text-xs focus:border-primary focus:outline-none"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-muted-foreground">Сетевой мост (bridge)</span>
                    <input
                      value={String(selectedNode.config.bridge ?? '')}
                      onChange={(e) => updateSelectedConfig('bridge', e.target.value)}
                      className="h-8 w-full rounded border border-border bg-background px-2 text-xs focus:border-primary focus:outline-none"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-muted-foreground">Шаблон (template)</span>
                    <input
                      value={String(selectedNode.config.template ?? '')}
                      onChange={(e) => updateSelectedConfig('template', e.target.value)}
                      className="h-8 w-full rounded border border-border bg-background px-2 text-xs focus:border-primary focus:outline-none"
                    />
                  </label>
                  <div className="grid grid-cols-3 gap-2">
                    <label className="flex flex-col gap-1">
                      <span className="text-muted-foreground">Ядра CPU</span>
                      <input
                        type="number"
                        min={1}
                        value={selectedNode.config.cores ?? ''}
                        onChange={(e) =>
                          updateSelectedConfig('cores', e.target.value === '' ? '' : Number(e.target.value))
                        }
                        className="h-8 w-full rounded border border-border bg-background px-2 font-mono text-xs focus:border-primary focus:outline-none"
                      />
                    </label>
                    <label className="flex flex-col gap-1">
                      <span className="text-muted-foreground">ОЗУ, МиБ</span>
                      <input
                        type="number"
                        min={128}
                        step={128}
                        value={selectedNode.config.memory ?? ''}
                        onChange={(e) =>
                          updateSelectedConfig('memory', e.target.value === '' ? '' : Number(e.target.value))
                        }
                        className="h-8 w-full rounded border border-border bg-background px-2 font-mono text-xs focus:border-primary focus:outline-none"
                      />
                    </label>
                    <label className="flex flex-col gap-1">
                      <span className="text-muted-foreground">Диск, ГиБ</span>
                      <input
                        type="number"
                        min={1}
                        value={selectedNode.config.disk ?? ''}
                        onChange={(e) =>
                          updateSelectedConfig('disk', e.target.value === '' ? '' : Number(e.target.value))
                        }
                        className="h-8 w-full rounded border border-border bg-background px-2 font-mono text-xs focus:border-primary focus:outline-none"
                      />
                    </label>
                  </div>
                </>
              )}

              {selectedNode.kind === 'network' && (
                <label className="flex flex-col gap-1">
                  <span className="text-muted-foreground">Сетевой мост (bridge)</span>
                  <input
                    value={String(selectedNode.config.bridge ?? '')}
                    onChange={(e) => updateSelectedConfig('bridge', e.target.value)}
                    className="h-8 w-full rounded border border-border bg-background px-2 text-xs focus:border-primary focus:outline-none"
                  />
                </label>
              )}

              {selectedNode.kind === 'storage' && (
                <label className="flex flex-col gap-1">
                  <span className="text-muted-foreground">Хранилище (datastoreId)</span>
                  <input
                    value={String(selectedNode.config.datastoreId ?? '')}
                    onChange={(e) => updateSelectedConfig('datastoreId', e.target.value)}
                    className="h-8 w-full rounded border border-border bg-background px-2 text-xs focus:border-primary focus:outline-none"
                  />
                </label>
              )}

              {selectedNode.kind === 'template' && (
                <label className="flex flex-col gap-1">
                  <span className="text-muted-foreground">Идентификатор шаблона (templateId)</span>
                  <input
                    value={String(selectedNode.config.templateId ?? '')}
                    onChange={(e) => updateSelectedConfig('templateId', e.target.value)}
                    className="h-8 w-full rounded border border-border bg-background px-2 text-xs focus:border-primary focus:outline-none"
                  />
                </label>
              )}
            </div>
          ) : (
            <p className="mt-4 text-center text-xs text-muted-foreground">
              Выберите узел на схеме для настройки параметров или удалите его.
            </p>
          )}
        </div>
      </div>

      {/* Подсказка о связях топологии */}
      <div className="flex items-center gap-2 rounded-md border border-border/70 bg-card/60 px-3 py-2 text-xs text-muted-foreground">
        <Info className="size-4 shrink-0 text-primary" />
        <span>
          Связи между блоками определяют зависимости и подключения инфраструктуры (например, привязку ВМ к сети, хранилищу или базовому шаблону).
        </span>
      </div>
    </div>
  )
}

export default InfrastructureDesigner
