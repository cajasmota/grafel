import request from 'supertest';
import { INestApplication } from '@nestjs/common';
import { buildJwtFixtures } from './support/jwt-helper';
import { buildAuthTestApp, InMemoryPageGrantRepository } from './support/auth-test-module';
import { closeE2eApp } from './support/e2e-app';
import type { JwtFixtures } from './support/jwt-helper';

const TEST_SUB = 'e2e-user-sub';
const GROUP_A = 'group-a';
const GROUP_B = 'group-b';

describe('AuthGuard — HTTP e2e (F7c)', () => {
  let app: INestApplication;
  let fixtures: JwtFixtures;
  let grantRepo: InMemoryPageGrantRepository;

  beforeAll(async () => {
    fixtures = await buildJwtFixtures();
    const built = await buildAuthTestApp(fixtures);
    app = built.app;
    grantRepo = built.grantRepo;
  });

  afterAll(async () => {
    await closeE2eApp(app);
  });

  afterEach(() => {
    grantRepo.clear();
  });

  describe('@Public route', () => {
    it('200 with no Authorization header', () => {
      return request(app.getHttpServer()).get('/probe/public').expect(200);
    });
  });

  describe('401 — unauthenticated', () => {
    it('401 when Authorization header is absent', () => {
      return request(app.getHttpServer()).get('/probe/buildings').expect(401);
    });

    it('401 for a malformed token string', () => {
      return request(app.getHttpServer()).get('/probe/buildings').set('Authorization', 'Bearer not.a.real.jwt').expect(401);
    });

    it('401 for an expired token', async () => {
      const token = await fixtures.mintToken({ expiredSecondsAgo: 3600 });
      return request(app.getHttpServer()).get('/probe/buildings').set('Authorization', `Bearer ${token}`).expect(401);
    });
  });

  describe('403 — authenticated, insufficient grants', () => {
    it('403 when user has no grant for the required page', async () => {
      const token = await fixtures.mintToken({ group: GROUP_A });
      return request(app.getHttpServer()).get('/probe/buildings').set('Authorization', `Bearer ${token}`).set('X-Group-Id', GROUP_A).expect(403);
    });

    it('403 when grant exists for a DIFFERENT group (group-scoping proof at the wire)', async () => {
      grantRepo.seed(TEST_SUB, GROUP_B, [{ page: 'buildings', read: true, write: true }]);
      const token = await fixtures.mintToken({ group: GROUP_A });
      return request(app.getHttpServer()).get('/probe/buildings').set('Authorization', `Bearer ${token}`).set('X-Group-Id', GROUP_A).expect(403);
    });

    it('403 when write method used but grant is read-only', async () => {
      grantRepo.seed(TEST_SUB, GROUP_A, [{ page: 'buildings', read: true, write: false }]);
      const token = await fixtures.mintToken({ group: GROUP_A });
      return request(app.getHttpServer()).post('/probe/buildings').set('Authorization', `Bearer ${token}`).set('X-Group-Id', GROUP_A).expect(403);
    });
  });

  describe('200 — authenticated with valid grant', () => {
    it('200 on GET when user has read grant for the correct group', async () => {
      grantRepo.seed(TEST_SUB, GROUP_A, [{ page: 'buildings', read: true, write: false }]);
      const token = await fixtures.mintToken({ group: GROUP_A });
      return request(app.getHttpServer()).get('/probe/buildings').set('Authorization', `Bearer ${token}`).set('X-Group-Id', GROUP_A).expect(200);
    });

    it('200 on POST when user has write grant for the correct group', async () => {
      grantRepo.seed(TEST_SUB, GROUP_A, [{ page: 'buildings', read: false, write: true }]);
      const token = await fixtures.mintToken({ group: GROUP_A });
      return request(app.getHttpServer()).post('/probe/buildings').set('Authorization', `Bearer ${token}`).set('X-Group-Id', GROUP_A).expect(200);
    });
  });

  describe('@RequireAction — operation-level (type-2) tier at the wire', () => {
    it('403 when the user holds the surrounding page grant but NOT the named action grant', async () => {
      grantRepo.seed(TEST_SUB, GROUP_A, [{ page: 'devices', read: true, write: true }]);
      const token = await fixtures.mintToken({ group: GROUP_A });
      return request(app.getHttpServer()).get('/probe/devices-lite').set('Authorization', `Bearer ${token}`).set('X-Group-Id', GROUP_A).expect(403);
    });

    it('403 when the action grant exists only in a DIFFERENT group', async () => {
      grantRepo.seedActions(TEST_SUB, GROUP_B, [{ action: 'lite', read: true, write: true }]);
      const token = await fixtures.mintToken({ group: GROUP_A });
      return request(app.getHttpServer()).get('/probe/devices-lite').set('Authorization', `Bearer ${token}`).set('X-Group-Id', GROUP_A).expect(403);
    });

    it('200 when the active group holds the matching action grant', async () => {
      grantRepo.seedActions(TEST_SUB, GROUP_A, [{ action: 'lite', read: true, write: false }]);
      const token = await fixtures.mintToken({ group: GROUP_A });
      return request(app.getHttpServer()).get('/probe/devices-lite').set('Authorization', `Bearer ${token}`).set('X-Group-Id', GROUP_A).expect(200);
    });

    it('composition: holding only the page grant is 403 — the action grant is also required', async () => {
      grantRepo.seed(TEST_SUB, GROUP_A, [{ page: 'devices', read: true, write: true }]);
      const token = await fixtures.mintToken({ group: GROUP_A });
      return request(app.getHttpServer())
        .get('/probe/devices-lite-composed')
        .set('Authorization', `Bearer ${token}`)
        .set('X-Group-Id', GROUP_A)
        .expect(403);
    });

    it('composition: holding only the action grant is 403 — the page grant is also required', async () => {
      grantRepo.seedActions(TEST_SUB, GROUP_A, [{ action: 'lite', read: true, write: true }]);
      const token = await fixtures.mintToken({ group: GROUP_A });
      return request(app.getHttpServer())
        .get('/probe/devices-lite-composed')
        .set('Authorization', `Bearer ${token}`)
        .set('X-Group-Id', GROUP_A)
        .expect(403);
    });

    it('composition: 200 only when BOTH the page and the action grant are held', async () => {
      grantRepo.seed(TEST_SUB, GROUP_A, [{ page: 'devices', read: true, write: true }]);
      grantRepo.seedActions(TEST_SUB, GROUP_A, [{ action: 'lite', read: true, write: true }]);
      const token = await fixtures.mintToken({ group: GROUP_A });
      return request(app.getHttpServer())
        .get('/probe/devices-lite-composed')
        .set('Authorization', `Bearer ${token}`)
        .set('X-Group-Id', GROUP_A)
        .expect(200);
    });
  });

  describe('@RequireAnyPage — OR over page grants at the wire', () => {
    it('403 when the user holds NONE of the listed pages', async () => {
      grantRepo.seed(TEST_SUB, GROUP_A, [{ page: 'buildings', read: true, write: true }]);
      const token = await fixtures.mintToken({ group: GROUP_A });
      return request(app.getHttpServer()).get('/probe/to-reschedule').set('Authorization', `Bearer ${token}`).set('X-Group-Id', GROUP_A).expect(403);
    });

    it('200 when the user holds the FIRST listed page', async () => {
      grantRepo.seed(TEST_SUB, GROUP_A, [{ page: 'inspection-results', read: true, write: false }]);
      const token = await fixtures.mintToken({ group: GROUP_A });
      return request(app.getHttpServer()).get('/probe/to-reschedule').set('Authorization', `Bearer ${token}`).set('X-Group-Id', GROUP_A).expect(200);
    });

    it('200 when the user holds the SECOND listed page (OR)', async () => {
      grantRepo.seed(TEST_SUB, GROUP_A, [{ page: 'scheduling', read: true, write: false }]);
      const token = await fixtures.mintToken({ group: GROUP_A });
      return request(app.getHttpServer()).get('/probe/to-reschedule').set('Authorization', `Bearer ${token}`).set('X-Group-Id', GROUP_A).expect(200);
    });
  });
});
