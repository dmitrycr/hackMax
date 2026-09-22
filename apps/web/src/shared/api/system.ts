export type ComponentStatus = { name: string; status: 'ready' | 'unavailable' | 'not_implemented' };
export type SystemStatus = { service: string; stage: string; components: ComponentStatus[] };

export async function getSystemStatus(signal: AbortSignal): Promise<SystemStatus> {
  const response = await fetch('/api/v1/system', { signal, cache: 'no-store' });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  return response.json() as Promise<SystemStatus>;
}

