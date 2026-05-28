// NestJS dto_extraction fixture (#2904). Hand-written, dependency-free.
// A NestJS controller method binds a typed DTO via the @Body() parameter
// decorator, plus @Query()/@Param() variants, proving the dto_extraction
// VALIDATES edges (method → dto:<TypeName>).
import { Controller, Post, Get, Body, Query, Param } from '@nestjs/common'

class CreateUserDto {
  name!: string
  age!: number
}

class ListUsersQueryDto {
  page!: number
}

@Controller('users')
export class UsersController {
  @Post()
  create(@Body() dto: CreateUserDto) {
    return dto
  }

  @Get()
  list(@Query() query: ListUsersQueryDto, @Param('id') id: string) {
    return { query, id }
  }
}
