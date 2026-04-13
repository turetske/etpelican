import { ServerGeneral } from '@/types';

const serverHasError = (server?: ServerGeneral) => {
  // Caches that disable director tests should always be flagged as having an
  // error because we want to disincentivize caches from hiding their status.
  if (server?.disableDirectorTest && server?.type === 'Cache') {
    return true;
  }

  // Origins that disable director tests (common for S3, Globus, etc.) should
  // not show as red for transient statuses like 'warning' that commonly appear
  // during startup. However, 'critical', 'degraded', and 'shutting down' are
  // serious enough to warrant the error indicator regardless.
  if (server?.disableDirectorTest && server?.type === 'Origin') {
    return ['critical', 'degraded', 'shutting down'].includes(
      server?.serverStatus || ''
    );
  }

  return (
    server?.healthStatus === 'Error' ||
    ['shutting down', 'critical', 'degraded', 'warning'].includes(
      server?.serverStatus || ''
    )
  );
};

export default serverHasError;
