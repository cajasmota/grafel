// NestJS middleware_coverage fixture (#2853). Hand-written, manifest-free.
// Covers the NestJS interceptor/pipe/filter/guard pipeline triad at both the
// class level (applies to every handler) and the method level.
import { Controller, Get, Post } from '@nestjs/common'
import { UseInterceptors, UsePipes, UseFilters, UseGuards } from '@nestjs/common'

@Controller('orders')
@UseInterceptors(LoggingInterceptor)
@UseFilters(HttpExceptionFilter)
export class OrdersController {
  @Get()
  findAll() {
    return []
  }

  @Post()
  @UsePipes(ValidationPipe)
  @UseGuards(RolesGuard)
  create() {
    return {}
  }
}
