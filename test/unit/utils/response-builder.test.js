/**
 * Response Builder 单元测试
 */
import { describe, it, expect, vi, beforeAll } from 'vitest';

let responseBuilder;

beforeAll(async () => {
  responseBuilder = await import('../../../src/utils/response-builder.js');
});

describe('response-builder', () => {
  describe('success', () => {
    it('should build success response with data', () => {
      const result = responseBuilder.success({ id: 1, name: 'test' });
      expect(result).toEqual({
        success: true,
        data: { id: 1, name: 'test' },
      });
    });

    it('should include message if provided', () => {
      const result = responseBuilder.success({ id: 1 }, 'Created successfully');
      expect(result).toEqual({
        success: true,
        data: { id: 1 },
        message: 'Created successfully',
      });
    });
  });

  describe('error', () => {
    it('should build error response', () => {
      const result = responseBuilder.error('VALIDATION_ERROR', 'Invalid input');
      expect(result).toEqual({
        success: false,
        error: {
          code: 'VALIDATION_ERROR',
          message: 'Invalid input',
        },
      });
    });

    it('should include details if provided', () => {
      const result = responseBuilder.error('VALIDATION_ERROR', 'Invalid input', { field: 'email' });
      expect(result.error.details).toEqual({ field: 'email' });
    });

    it('should include requestId if provided', () => {
      const result = responseBuilder.error('BAD', 'Bad thing', undefined, 'req-1');
      expect(result.requestId).toBe('req-1');
    });
  });

  describe('paginated', () => {
    it('should build paginated response', () => {
      const items = [{ id: 1 }, { id: 2 }];
      const result = responseBuilder.paginated(items, { page: 1, pageSize: 10, total: 25 });

      expect(result.success).toBe(true);
      expect(result.data.items).toEqual(items);
      expect(result.data.pagination).toEqual({
        page: 1,
        pageSize: 10,
        total: 25,
        totalPages: 3,
        hasMore: true,
      });
    });

    it('should calculate hasMore correctly', () => {
      const items = [];
      const result = responseBuilder.paginated(items, { page: 3, pageSize: 10, total: 25 });
      expect(result.data.pagination.hasMore).toBe(false);
    });
  });

  describe('sendSuccess', () => {
    it('should send success response with correct status', () => {
      const mockRes = {
        status: vi.fn().mockReturnThis(),
        json: vi.fn(),
      };

      responseBuilder.sendSuccess(mockRes, { id: 1 }, 201);

      expect(mockRes.status).toHaveBeenCalledWith(201);
      expect(mockRes.json).toHaveBeenCalledWith({
        success: true,
        data: { id: 1 },
      });
    });

    it('should default to status 200', () => {
      const mockRes = {
        status: vi.fn().mockReturnThis(),
        json: vi.fn(),
      };

      responseBuilder.sendSuccess(mockRes, {});

      expect(mockRes.status).toHaveBeenCalledWith(200);
    });
  });

  describe('sendError', () => {
    it('should send error response with correct status', () => {
      const mockRes = {
        status: vi.fn().mockReturnThis(),
        json: vi.fn(),
      };

      responseBuilder.sendError(mockRes, 400, 'BAD_REQUEST', 'Invalid data');

      expect(mockRes.status).toHaveBeenCalledWith(400);
      expect(mockRes.json).toHaveBeenCalledWith({
        success: false,
        error: {
          code: 'BAD_REQUEST',
          message: 'Invalid data',
        },
      });
    });
  });

  describe('errors helpers', () => {
    it('badRequest should send 400', () => {
      const mockRes = { status: vi.fn().mockReturnThis(), json: vi.fn() };
      responseBuilder.errors.badRequest(mockRes);
      expect(mockRes.status).toHaveBeenCalledWith(400);
    });

    it('unauthorized should send 401', () => {
      const mockRes = { status: vi.fn().mockReturnThis(), json: vi.fn() };
      responseBuilder.errors.unauthorized(mockRes);
      expect(mockRes.status).toHaveBeenCalledWith(401);
    });

    it('forbidden should send 403', () => {
      const mockRes = { status: vi.fn().mockReturnThis(), json: vi.fn() };
      responseBuilder.errors.forbidden(mockRes);
      expect(mockRes.status).toHaveBeenCalledWith(403);
    });

    it('notFound should send 404', () => {
      const mockRes = { status: vi.fn().mockReturnThis(), json: vi.fn() };
      responseBuilder.errors.notFound(mockRes);
      expect(mockRes.status).toHaveBeenCalledWith(404);
    });

    it('validationError should send 422', () => {
      const mockRes = { status: vi.fn().mockReturnThis(), json: vi.fn() };
      responseBuilder.errors.validationError(mockRes, 'Invalid', { field: 'email' });
      expect(mockRes.status).toHaveBeenCalledWith(422);
    });

    it('tooManyRequests should send 429', () => {
      const mockRes = { status: vi.fn().mockReturnThis(), json: vi.fn() };
      responseBuilder.errors.tooManyRequests(mockRes);
      expect(mockRes.status).toHaveBeenCalledWith(429);
    });

    it('internal should send 500', () => {
      const mockRes = { status: vi.fn().mockReturnThis(), json: vi.fn() };
      responseBuilder.errors.internal(mockRes);
      expect(mockRes.status).toHaveBeenCalledWith(500);
    });
  });
});
