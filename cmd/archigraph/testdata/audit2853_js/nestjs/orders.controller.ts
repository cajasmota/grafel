// audit2853_js — NestJS slice. Interceptor/pipe/filter/guard pipeline triad.
// Route shapes mirror the proven #2852 auth corpus so the full indexer pipeline
// synthesizes both handlers; the pipeline decorators carry the middleware chain.
import { Controller, Get, Post, UseInterceptors, UsePipes, UseFilters, UseGuards } from '@nestjs/common'

@Controller('orders')
@UseInterceptors(LoggingInterceptor)
@UseFilters(HttpExceptionFilter)
export class OrdersController {
  @Get()
  findAll() {}

  @Post()
  @UsePipes(ValidationPipe)
  @UseGuards(RolesGuard)
  create() {}
}
