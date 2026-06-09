import { NotFoundException } from '@nestjs/common';
import { CreateNotificationService } from './create-notification.service';
import { CreateNotificationRepository } from '../repositories/create-notification.repository';
import { UserRepository } from '../../users/repositories/user.repository';
import { Notification } from '../../notifications/models/notification.entity';
import { User } from '../../users/models/user.entity';

function makeUser(id: number): User {
  return Object.assign(new User(), { id, cognitoId: `sub-${id}` });
}

function makeNotification(id: number, userId: number): Notification {
  return Object.assign(new Notification(), {
    id,
    userId,
    title: '',
    message: '',
    status: null,
    href: null,
    payload: {},
    dateRead: null,
    created: new Date('2026-01-01T00:00:00Z'),
  });
}

describe('CreateNotificationService', () => {
  let notifications: jest.Mocked<Pick<CreateNotificationRepository, 'create' | 'findByUserAndId' | 'save'>>;
  let users: jest.Mocked<Pick<UserRepository, 'findByCognitoId'>>;
  let service: CreateNotificationService;

  beforeEach(() => {
    notifications = { create: jest.fn(), findByUserAndId: jest.fn(), save: jest.fn() };
    users = { findByCognitoId: jest.fn() };
    service = new CreateNotificationService(notifications as unknown as CreateNotificationRepository, users as unknown as UserRepository);
  });

  describe('create', () => {
    it('creates a notification for the resolved user and returns success', async () => {
      const user = makeUser(7);
      users.findByCognitoId.mockResolvedValue(user);
      const created = makeNotification(1, 7);
      notifications.create.mockResolvedValue(created);

      const result = await service.create('sub-7', { title: 'Hello', message: 'World', status: 'active', href: '/x', payload: { a: 1 } });

      expect(result).toStrictEqual({ success: true });
      expect(notifications.create).toHaveBeenCalledWith(
        expect.objectContaining({ userId: 7, title: 'Hello', message: 'World', status: 'active', href: '/x', payload: { a: 1 } }),
      );
    });

    it('applies defaults when optional fields are omitted', async () => {
      users.findByCognitoId.mockResolvedValue(makeUser(3));
      notifications.create.mockResolvedValue(makeNotification(2, 3));

      await service.create('sub-3', {});

      expect(notifications.create).toHaveBeenCalledWith(expect.objectContaining({ title: '', message: '', status: null, href: null, payload: {} }));
    });

    it('throws NotFoundException when user is not found', async () => {
      users.findByCognitoId.mockResolvedValue(null);

      await expect(service.create('no-such-sub', {})).rejects.toThrow(NotFoundException);
    });
  });

  describe('markRead', () => {
    it('sets dateRead if currently null and returns success', async () => {
      const user = makeUser(5);
      users.findByCognitoId.mockResolvedValue(user);
      const notification = makeNotification(10, 5);
      notifications.findByUserAndId.mockResolvedValue(notification);
      notifications.save.mockResolvedValue(notification);

      const result = await service.markRead('sub-5', 10);

      expect(result).toStrictEqual({ success: true });
      expect(notification.dateRead).not.toBeNull();
      expect(notifications.save).toHaveBeenCalled();
    });

    it('does not update dateRead if already set (idempotent)', async () => {
      users.findByCognitoId.mockResolvedValue(makeUser(5));
      const alreadyRead = makeNotification(10, 5);
      alreadyRead.dateRead = new Date('2026-01-01T00:00:00Z');
      notifications.findByUserAndId.mockResolvedValue(alreadyRead);

      const result = await service.markRead('sub-5', 10);

      expect(result).toStrictEqual({ success: true });
      expect(notifications.save).not.toHaveBeenCalled();
    });

    it('throws NotFoundException when notification is not found', async () => {
      users.findByCognitoId.mockResolvedValue(makeUser(5));
      notifications.findByUserAndId.mockResolvedValue(null);

      await expect(service.markRead('sub-5', 999)).rejects.toThrow(NotFoundException);
    });

    it('throws NotFoundException when user is not found', async () => {
      users.findByCognitoId.mockResolvedValue(null);

      await expect(service.markRead('no-such-sub', 1)).rejects.toThrow(NotFoundException);
    });
  });
});
