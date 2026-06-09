import request from 'supertest';
import { INestApplication, VersioningType } from '@nestjs/common';
import { Test } from '@nestjs/testing';
import { ConfigService } from '@nestjs/config';
import { APP_FILTER, APP_GUARD, APP_INTERCEPTOR, APP_PIPE } from '@nestjs/core';
import { AllExceptionsFilter } from '../src/common/exceptions/all-exceptions/all-exceptions.filter';
import { EnvelopeInterceptor } from '../src/common/envelope/interceptor/envelope.interceptor';
import { ErrorShapeInterceptor } from '../src/common/validation/error-shape-interceptor/error-shape.interceptor';
import { createValidationPipe } from '../src/common/validation/validation-pipe/validation-pipe.factory';
import { AuthGuard } from '../src/common/auth/guards/auth.guard';
import { CognitoTokenValidator } from '../src/common/auth/token-validator/cognito-token.validator';
import { PagePermissionResolver } from '../src/common/auth/page/page-permission.resolver';
import { ActionPermissionResolver } from '../src/common/auth/action/action-permission.resolver';
import { PrincipalFactory } from '../src/common/auth/page/principal.factory';
import { PageGrantRepository } from '../src/common/auth/persistence/page-grant.repository';
import { RolePermission } from '../src/common/auth/persistence/role-permission.entity';
import { getRepositoryToken } from '@nestjs/typeorm';
import { AlternateAddressController } from '../src/modules/alternate-addresses/api/alternate-address.controller';
import { AlternateAddressService } from '../src/modules/alternate-addresses/services/alternate-address.service';
import { AlternateAddressRepository } from '../src/modules/alternate-addresses/repositories/alternate-address.repository';
import { AlternateAddress } from '../src/modules/alternate-addresses/models/alternate-address.entity';
import { buildJwtFixtures, E2E_ISSUER, E2E_AUDIENCE } from './support/jwt-helper';
import { InMemoryPageGrantRepository } from './support/auth-test-module';
import { initE2eApp, sealE2eApp, closeE2eApp } from './support/e2e-app';
import type { JwtFixtures } from './support/jwt-helper';

const TEST_SUB = 'e2e-user-sub';
const GROUP_A = 'group-a';
const ROUTE = '/api/v1/alternate-addresses';

class InMemoryAlternateAddressRepo {
  private seq = 0;
  private readonly rows = new Map<number, AlternateAddress>();

  create(attributes: Partial<AlternateAddress>): Promise<AlternateAddress> {
    const entity = new AlternateAddress();
    entity.id = ++this.seq;
    entity.buildingId = attributes.buildingId as number;
    entity.groupId = attributes.groupId ?? null;
    entity.address = attributes.address as string;
    entity.createdAt = new Date('2024-01-01T00:00:00Z');
    entity.updatedAt = new Date('2024-01-01T00:00:00Z');
    entity.deletedAt = null;
    this.rows.set(entity.id, entity);
    return Promise.resolve(entity);
  }

  applyUpdate(id: number, attributes: Partial<AlternateAddress>): Promise<AlternateAddress | null> {
    const existing = this.rows.get(id);
    if (!existing) return Promise.resolve(null);
    Object.assign(existing, attributes);
    return Promise.resolve(existing);
  }

  softDelete(id: number): Promise<boolean> {
    return Promise.resolve(this.rows.delete(id));
  }
}

interface ErrorEnvelope {
  [field: string]: string[];
}

describe('AlternateAddressController — write HTTP e2e (F8c, issue #19 lifecycle proof)', () => {
  let app: INestApplication;
  let fixtures: JwtFixtures;
  let grantRepo: InMemoryPageGrantRepository;

  beforeAll(async () => {
    fixtures = await buildJwtFixtures();
    grantRepo = new InMemoryPageGrantRepository();

    const moduleRef = await Test.createTestingModule({
      controllers: [AlternateAddressController],
      providers: [
        AlternateAddressService,
        { provide: AlternateAddressRepository, useClass: InMemoryAlternateAddressRepo },
        { provide: APP_FILTER, useClass: AllExceptionsFilter },
        { provide: APP_INTERCEPTOR, useClass: ErrorShapeInterceptor },
        { provide: APP_INTERCEPTOR, useClass: EnvelopeInterceptor },
        { provide: APP_PIPE, useFactory: createValidationPipe },
        { provide: APP_GUARD, useClass: AuthGuard },
        PagePermissionResolver,
        ActionPermissionResolver,
        PrincipalFactory,
        {
          provide: CognitoTokenValidator,
          useFactory: () => {
            const config = {
              get: (key: string): string => {
                if (key === 'COGNITO_ISSUER') return E2E_ISSUER;
                if (key === 'COGNITO_AUDIENCE') return E2E_AUDIENCE;
                return '';
              },
            } as ConfigService;
            return new CognitoTokenValidator(config, fixtures.keyFetcher);
          },
        },
        { provide: PageGrantRepository, useValue: grantRepo },
        { provide: getRepositoryToken(RolePermission), useValue: {} },
      ],
    }).compile();

    app = initE2eApp(moduleRef);
    app.setGlobalPrefix('api', { exclude: ['health'] });
    app.enableVersioning({ type: VersioningType.URI, defaultVersion: '1' });
    await sealE2eApp(app);
  });

  afterAll(async () => {
    await closeE2eApp(app);
  });

  afterEach(() => {
    grantRepo.clear();
  });

  async function writeToken(): Promise<string> {
    grantRepo.seed(TEST_SUB, GROUP_A, [{ page: 'buildings', read: true, write: true }]);
    return fixtures.mintToken({ group: GROUP_A });
  }

  describe('auth on the write routes', () => {
    it('401 when no bearer token is supplied', () => {
      return request(app.getHttpServer()).post(ROUTE).send({ building: 1, group: 2, address: 'x' }).expect(401);
    });

    it('403 when the principal has only a read grant (write bit missing)', async () => {
      grantRepo.seed(TEST_SUB, GROUP_A, [{ page: 'buildings', read: true, write: false }]);
      const token = await fixtures.mintToken({ group: GROUP_A });
      return request(app.getHttpServer())
        .post(ROUTE)
        .set('Authorization', `Bearer ${token}`)
        .set('X-Group-Id', GROUP_A)
        .send({ building: 1, group: 2, address: 'x' })
        .expect(403);
    });
  });

  describe('validation-400 over the wire (the #19 proof)', () => {
    it('returns 400 with the bare field-error map when address is missing', async () => {
      const token = await writeToken();

      const res = await request(app.getHttpServer())
        .post(ROUTE)
        .set('Authorization', `Bearer ${token}`)
        .set('X-Group-Id', GROUP_A)
        .send({ building: 10, group: 20 })
        .expect(400);

      const body = res.body as ErrorEnvelope;
      expect(body).not.toHaveProperty('success');
      expect(body).not.toHaveProperty('message');
      expect(Array.isArray(body.address)).toBe(true);
      expect(typeof body.address[0]).toBe('string');
    });

    it('reports every missing required field as its own key', async () => {
      const token = await writeToken();

      const res = await request(app.getHttpServer())
        .post(ROUTE)
        .set('Authorization', `Bearer ${token}`)
        .set('X-Group-Id', GROUP_A)
        .send({})
        .expect(400);

      const body = res.body as ErrorEnvelope;
      expect(body).toHaveProperty('building');
      expect(body).toHaveProperty('group');
      expect(body).toHaveProperty('address');
    });

    it('returns 400 when address exceeds the max length', async () => {
      const token = await writeToken();

      const res = await request(app.getHttpServer())
        .post(ROUTE)
        .set('Authorization', `Bearer ${token}`)
        .set('X-Group-Id', GROUP_A)
        .send({ building: 10, group: 20, address: 'x'.repeat(256) })
        .expect(400);

      expect(Array.isArray((res.body as ErrorEnvelope).address)).toBe(true);
    });
  });

  describe('success lifecycle', () => {
    it('201 on a valid create, returning the record inside the success envelope', async () => {
      const token = await writeToken();

      const res = await request(app.getHttpServer())
        .post(ROUTE)
        .set('Authorization', `Bearer ${token}`)
        .set('X-Group-Id', GROUP_A)
        .send({ building: 10, group: 20, address: '5 Main St' })
        .expect(201);

      const body = res.body as { success: boolean; response: Record<string, unknown> };
      expect(body.success).toBe(true);
      expect(body.response).toMatchObject({ building: 10, group: 20, address: '5 Main St' });
      expect(body.response).toHaveProperty('id');
      expect(body.response).toHaveProperty('created_at');
    });

    it('200 on a valid PATCH update of an existing record', async () => {
      const token = await writeToken();

      const created = await request(app.getHttpServer())
        .post(ROUTE)
        .set('Authorization', `Bearer ${token}`)
        .set('X-Group-Id', GROUP_A)
        .send({ building: 10, group: 20, address: '5 Main St' })
        .expect(201);

      const id = (created.body as { response: { id: number } }).response.id;

      const res = await request(app.getHttpServer())
        .patch(`${ROUTE}/${id}`)
        .set('Authorization', `Bearer ${token}`)
        .set('X-Group-Id', GROUP_A)
        .send({ address: '7 Oak Ave' })
        .expect(200);

      expect((res.body as { response: { address: string } }).response.address).toBe('7 Oak Ave');
    });

    it('204 on delete of an existing record', async () => {
      const token = await writeToken();

      const created = await request(app.getHttpServer())
        .post(ROUTE)
        .set('Authorization', `Bearer ${token}`)
        .set('X-Group-Id', GROUP_A)
        .send({ building: 10, group: 20, address: '5 Main St' })
        .expect(201);

      const id = (created.body as { response: { id: number } }).response.id;

      await request(app.getHttpServer()).delete(`${ROUTE}/${id}`).set('Authorization', `Bearer ${token}`).set('X-Group-Id', GROUP_A).expect(204);
    });

    it('404 on update of a missing record', async () => {
      const token = await writeToken();

      return request(app.getHttpServer())
        .patch(`${ROUTE}/9999`)
        .set('Authorization', `Bearer ${token}`)
        .set('X-Group-Id', GROUP_A)
        .send({ address: 'nope' })
        .expect(404);
    });
  });
});
