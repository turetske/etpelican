import serverHasError from '@/helpers/serverHasError';
import { ServerGeneral } from '@/types';

const makeServer = (
  overrides: Partial<ServerGeneral> = {}
): ServerGeneral => ({
  name: 'test-server',
  version: '7.26.0',
  storageType: 'posix',
  disableDirectorTest: false,
  authUrl: '',
  brokerUrl: '',
  url: 'https://test.example.com',
  webUrl: 'https://test.example.com',
  type: 'Origin',
  latitude: 0,
  longitude: 0,
  capabilities: {
    PublicRead: false,
    Read: true,
    Write: false,
    Listing: false,
    FallBackRead: false,
  },
  filtered: false,
  filteredType: '',
  fromTopology: false,
  healthStatus: 'OK',
  serverStatus: 'ok',
  ioLoad: 0,
  namespacePrefixes: ['/test'],
  ...overrides,
});

describe('serverHasError', () => {
  test('returns false for healthy server', () => {
    expect(serverHasError(makeServer())).toBe(false);
  });

  test('returns true when healthStatus is Error', () => {
    expect(
      serverHasError(makeServer({ healthStatus: 'Error' }))
    ).toBe(true);
  });

  test('returns true when serverStatus is critical', () => {
    expect(
      serverHasError(makeServer({ serverStatus: 'critical' }))
    ).toBe(true);
  });

  test('returns true when serverStatus is warning', () => {
    expect(
      serverHasError(makeServer({ serverStatus: 'warning' }))
    ).toBe(true);
  });

  test('returns true when serverStatus is degraded', () => {
    expect(
      serverHasError(makeServer({ serverStatus: 'degraded' }))
    ).toBe(true);
  });

  test('returns false for undefined server', () => {
    expect(serverHasError(undefined)).toBe(false);
  });

  // Origins with disabled director tests
  test('origin with disabled tests and ok status is NOT an error', () => {
    expect(
      serverHasError(
        makeServer({
          type: 'Origin',
          disableDirectorTest: true,
          serverStatus: 'ok',
          healthStatus: 'Health Test Disabled',
        })
      )
    ).toBe(false);
  });

  test('origin with disabled tests and warning status is NOT an error', () => {
    expect(
      serverHasError(
        makeServer({
          type: 'Origin',
          disableDirectorTest: true,
          serverStatus: 'warning',
          healthStatus: 'Health Test Disabled',
        })
      )
    ).toBe(false);
  });

  test('origin with disabled tests and degraded status IS an error', () => {
    expect(
      serverHasError(
        makeServer({
          type: 'Origin',
          disableDirectorTest: true,
          serverStatus: 'degraded',
          healthStatus: 'Health Test Disabled',
        })
      )
    ).toBe(true);
  });

  test('origin with disabled tests and shutting down status IS an error', () => {
    expect(
      serverHasError(
        makeServer({
          type: 'Origin',
          disableDirectorTest: true,
          serverStatus: 'shutting down',
          healthStatus: 'Health Test Disabled',
        })
      )
    ).toBe(true);
  });

  test('origin with disabled tests and critical status IS an error', () => {
    expect(
      serverHasError(
        makeServer({
          type: 'Origin',
          disableDirectorTest: true,
          serverStatus: 'critical',
          healthStatus: 'Health Test Disabled',
        })
      )
    ).toBe(true);
  });

  // Caches with disabled director tests should always be flagged
  test('cache with disabled tests and ok status IS an error', () => {
    expect(
      serverHasError(
        makeServer({
          type: 'Cache',
          disableDirectorTest: true,
          serverStatus: 'ok',
          healthStatus: 'Health Test Disabled',
        })
      )
    ).toBe(true);
  });

  test('cache with disabled tests and warning status IS an error', () => {
    expect(
      serverHasError(
        makeServer({
          type: 'Cache',
          disableDirectorTest: true,
          serverStatus: 'warning',
          healthStatus: 'Health Test Disabled',
        })
      )
    ).toBe(true);
  });

  // Verify that enabled-test servers retain existing behavior
  test('origin with enabled tests and warning status IS an error', () => {
    expect(
      serverHasError(
        makeServer({
          type: 'Origin',
          disableDirectorTest: false,
          serverStatus: 'warning',
        })
      )
    ).toBe(true);
  });

  test('cache with enabled tests and ok status is NOT an error', () => {
    expect(
      serverHasError(
        makeServer({
          type: 'Cache',
          disableDirectorTest: false,
          serverStatus: 'ok',
          healthStatus: 'OK',
        })
      )
    ).toBe(false);
  });
});
