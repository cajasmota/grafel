import { Controller, Get, Param } from '@nestjs/common';
import { NotificationService } from './notification.service';

/**
 * NotificationController exposes the broadcast surface over HTTP. It injects
 * NotificationService via constructor DI — the deploy-8 (#3970) case: the
 * NestJS custom extractor emits a SCOPE.Component/controller carrying the
 * INJECTED_INTO edge, while the generic AST extractor emits a co-located
 * SCOPE.Component/class node WITHOUT the edge. After same-ID dedup the
 * edge-bearing controller entity must survive so NotificationService
 * INJECTED_INTO NotificationController is reachable.
 */
@Controller('notifications')
export class NotificationController {
  constructor(private readonly notifications: NotificationService) {}

  @Get(':channelId')
  list(@Param('channelId') channelId: string): string {
    return channelId;
  }
}
