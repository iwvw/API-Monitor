const STATES = {
  UP: 'up',
  DOWN: 'down',
  PENDING_DOWN: 'pending_down',
  PENDING_UP: 'pending_up',
  MAINTENANCE: 'maintenance',
  PAUSED: 'paused',
  UNKNOWN: 'unknown',
};

function normalizeState(previous = {}) {
  const legacyMap = {
    ok: STATES.UP,
    pending: STATES.PENDING_DOWN,
    firing: STATES.DOWN,
    recovery: STATES.PENDING_UP,
  };

  return {
    state: legacyMap[previous.state || previous.status] || previous.state || STATES.UP,
    failCount: Number(previous.failCount || previous.fail_count || 0),
    recoverCount: Number(previous.recoverCount || previous.recoveryCount || previous.recover_count || 0),
    activeIncidentId: previous.activeIncidentId || previous.active_incident_id || null,
    lastTransitionAt: previous.lastTransitionAt || previous.last_transition_at || null,
    lastError: previous.lastError || previous.last_error || null,
    lastPing: previous.lastPing || previous.last_ping || 0,
  };
}

function transition(previous, checkResult, monitorConfig = {}, maintenanceContext = {}) {
  const now = new Date().toISOString();
  const state = normalizeState(previous);
  const ok = !!checkResult.ok;
  const downConfirmCount = Number(
    monitorConfig.downConfirmCount || monitorConfig.down_confirm_count || monitorConfig.confirmCount || 3
  );
  const upConfirmCount = Number(
    monitorConfig.upConfirmCount || monitorConfig.up_confirm_count || monitorConfig.confirmCount || 3
  );

  if (monitorConfig.active === false) {
    return {
      nextState: { ...state, state: STATES.PAUSED, lastTransitionAt: now },
      incidentAction: null,
      notificationAction: null,
    };
  }

  if (maintenanceContext.active) {
    return {
      nextState: { ...state, state: STATES.MAINTENANCE, lastTransitionAt: now },
      incidentAction: null,
      notificationAction: null,
    };
  }

  const next = {
    ...state,
    lastError: ok ? null : checkResult.message || checkResult.error || 'Unknown error',
    lastPing: ok ? checkResult.latencyMs || 0 : 0,
  };
  let incidentAction = null;
  let notificationAction = null;
  const suppressedActiveIncident = [STATES.MAINTENANCE, STATES.PAUSED].includes(state.state) && state.activeIncidentId;

  if (ok) {
    if ([STATES.DOWN, STATES.PENDING_UP].includes(state.state) || suppressedActiveIncident) {
      next.state = STATES.PENDING_UP;
      next.recoverCount = state.recoverCount + 1;
      if (next.recoverCount >= upConfirmCount) {
        next.state = STATES.UP;
        next.failCount = 0;
        next.recoverCount = 0;
        next.lastTransitionAt = now;
        incidentAction = { type: 'resolve' };
        notificationAction = { type: 'up' };
      }
    } else {
      next.state = STATES.UP;
      next.failCount = 0;
      next.recoverCount = 0;
    }
  } else if (suppressedActiveIncident) {
    next.state = STATES.DOWN;
    next.recoverCount = 0;
  } else if ([STATES.UP, STATES.PENDING_DOWN, STATES.UNKNOWN, STATES.MAINTENANCE, STATES.PAUSED].includes(state.state)) {
    next.state = STATES.PENDING_DOWN;
    next.failCount = state.state === STATES.PENDING_DOWN ? state.failCount + 1 : 1;
    next.recoverCount = 0;
    if (next.failCount >= downConfirmCount) {
      next.state = STATES.DOWN;
      next.lastTransitionAt = now;
      incidentAction = { type: 'open', cause: next.lastError };
      notificationAction = { type: 'down' };
    }
  } else {
    next.state = STATES.DOWN;
    next.recoverCount = 0;
  }

  return {
    nextState: next,
    incidentAction,
    notificationAction,
  };
}

module.exports = {
  STATES,
  normalizeState,
  transition,
};
