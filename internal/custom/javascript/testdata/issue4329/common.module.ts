import { MiddlewareConsumer, Module, NestModule } from '@nestjs/common';
import { APP_FILTER, APP_GUARD, APP_INTERCEPTOR, APP_PIPE } from '@nestjs/core';
import { AllExceptionsFilter } from './exceptions/all-exceptions/all-exceptions.filter';
import { EnvelopeInterceptor } from './envelope/interceptor/envelope.interceptor';
import { SnakeToCamelMiddleware } from './transform/snake-to-camel/snake-to-camel.middleware';
import { createValidationPipe } from './validation/validation-pipe/validation-pipe.factory';
import { ErrorShapeInterceptor } from './validation/error-shape-interceptor/error-shape.interceptor';
import { AuthGuard } from './auth/guards/auth.guard';
import { CognitoTokenValidator } from './auth/token-validator/cognito-token.validator';
import { PagePermissionResolver } from './auth/page/page-permission.resolver';
import { ActionPermissionResolver } from './auth/action/action-permission.resolver';
import { PrincipalFactory } from './auth/page/principal.factory';
import { AuthPersistenceModule } from './auth/persistence/auth-persistence.module';
import { AppConfigModule } from './config/app-config.module';
import { RequestContextMiddleware } from './request-context/middleware/request-context.middleware';
import { RequestContextInterceptor } from './request-context/interceptor/request-context.interceptor';

@Module({
  imports: [AppConfigModule, AuthPersistenceModule],
  providers: [
    { provide: APP_FILTER, useClass: AllExceptionsFilter },
    { provide: APP_INTERCEPTOR, useClass: ErrorShapeInterceptor },
    { provide: APP_INTERCEPTOR, useClass: EnvelopeInterceptor },
    { provide: APP_INTERCEPTOR, useClass: RequestContextInterceptor },
    { provide: APP_PIPE, useFactory: createValidationPipe },
    { provide: APP_GUARD, useClass: AuthGuard },
    CognitoTokenValidator,
    PrincipalFactory,
    PagePermissionResolver,
    ActionPermissionResolver,
  ],
})
export class CommonModule implements NestModule {
  configure(consumer: MiddlewareConsumer): void {
    consumer.apply(SnakeToCamelMiddleware, RequestContextMiddleware).forRoutes('*');
  }
}
