import { describe, it, expect } from 'vitest';

const stateMachine = await import('../../../modules/uptime-api/domain/state-machine.js');
const { STATES, transition } = stateMachine;

describe('uptime state machine', () => {
  it('enters down only after configured failures', () => {
    const first = transition(
      { state: STATES.UP },
      { ok: false, message: 'timeout' },
      { downConfirmCount: 2 }
    );

    expect(first.nextState.state).toBe(STATES.PENDING_DOWN);
    expect(first.notificationAction).toBeNull();

    const second = transition(first.nextState, { ok: false, message: 'timeout' }, { downConfirmCount: 2 });
    expect(second.nextState.state).toBe(STATES.DOWN);
    expect(second.notificationAction).toEqual({ type: 'down' });
    expect(second.incidentAction).toEqual({ type: 'open', cause: 'timeout' });
  });

  it('recovers only after configured successes', () => {
    const pending = transition(
      { state: STATES.DOWN, recoverCount: 0 },
      { ok: true, latencyMs: 24 },
      { upConfirmCount: 2 }
    );

    expect(pending.nextState.state).toBe(STATES.PENDING_UP);
    expect(pending.notificationAction).toBeNull();

    const recovered = transition(pending.nextState, { ok: true, latencyMs: 18 }, { upConfirmCount: 2 });
    expect(recovered.nextState.state).toBe(STATES.UP);
    expect(recovered.notificationAction).toEqual({ type: 'up' });
    expect(recovered.incidentAction).toEqual({ type: 'resolve' });
  });

  it('pauses without notification when monitor inactive', () => {
    const result = transition({ state: STATES.DOWN }, { ok: false }, { active: false });
    expect(result.nextState.state).toBe(STATES.PAUSED);
    expect(result.notificationAction).toBeNull();
  });

  it('re-enters pending down after maintenance without an active incident', () => {
    const result = transition(
      { state: STATES.MAINTENANCE },
      { ok: false, message: 'timeout' },
      { downConfirmCount: 2 }
    );

    expect(result.nextState.state).toBe(STATES.PENDING_DOWN);
    expect(result.nextState.failCount).toBe(1);
    expect(result.incidentAction).toBeNull();
    expect(result.notificationAction).toBeNull();
  });

  it('does not duplicate incidents for suppressed active incidents', () => {
    const stillDown = transition(
      { state: STATES.MAINTENANCE, activeIncidentId: 123 },
      { ok: false, message: 'timeout' },
      { downConfirmCount: 1 }
    );

    expect(stillDown.nextState.state).toBe(STATES.DOWN);
    expect(stillDown.incidentAction).toBeNull();
    expect(stillDown.notificationAction).toBeNull();

    const recovered = transition(
      { state: STATES.PAUSED, activeIncidentId: 123 },
      { ok: true, latencyMs: 20 },
      { upConfirmCount: 1 }
    );

    expect(recovered.nextState.state).toBe(STATES.UP);
    expect(recovered.incidentAction).toEqual({ type: 'resolve' });
    expect(recovered.notificationAction).toEqual({ type: 'up' });
  });
});
