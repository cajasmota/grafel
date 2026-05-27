// NestJS: decorator + method body live in the same file. The audit checks
// that endpoint source_line resolves to the method def line (not line 0 and
// not the @Controller line).

import { Controller, Get, Post, Param, Body } from "@nestjs/common";

interface OrderDto {
  id: string;
  total: number;
}

@Controller("orders")
export class OrdersController {
  @Get()
  listOrders(): OrderDto[] {
    return [{ id: "o1", total: 42 }];
  }

  @Post()
  createOrder(@Body() body: OrderDto): OrderDto {
    return body;
  }

  @Get(":id")
  getOrder(@Param("id") id: string): OrderDto {
    return { id, total: 0 };
  }
}
