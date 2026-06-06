export function shouldIgnoreRowDoubleClick(event) {
  const target = event?.target;
  if (!target || typeof target.closest !== 'function') return false;
  return Boolean(target.closest('button,a,input,select,textarea,[role="button"],[role="checkbox"],[role="menuitem"],[data-row-dblclick-ignore]'));
}

export function handleEditableRowDoubleClick(event, onEdit) {
  if (shouldIgnoreRowDoubleClick(event)) return;
  onEdit?.();
}
