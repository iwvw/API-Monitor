import { Activity, Clock, GitBranch, Server } from '../../components/Icons.jsx';

export const TASK_TABS = [
  { value: 'tasks', label: <span className="inline-flex items-center gap-1.5"><Clock className="w-3.5 h-3.5" />任务</span> },
  { value: 'workflows', label: <span className="inline-flex items-center gap-1.5"><GitBranch className="w-3.5 h-3.5" />工作流</span> },
  { value: 'runs', label: <span className="inline-flex items-center gap-1.5"><Activity className="w-3.5 h-3.5" />运行记录</span> },
  { value: 'nodes', label: <span className="inline-flex items-center gap-1.5"><Server className="w-3.5 h-3.5" />执行节点</span> },
];
