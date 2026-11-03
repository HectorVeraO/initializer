import { ApiQueryBuilder } from './api-query-builder';

describe('ApiQueryBuilder', () => {
  /* Pagination */

  it('should set page and size', () => {
    const params = new ApiQueryBuilder().setPage(2).setSize(25).build();

    expect(params).toEqual({ page: 2, size: 25 });
  });

  it('should keep page 1, the first page of the API', () => {
    const params = new ApiQueryBuilder().setPage(1).setSize(10).build();

    expect(params['page']).toBe(1);
  });

  it('should omit page/size when not set', () => {
    const params = new ApiQueryBuilder().build();

    expect(params['page']).toBeUndefined();
    expect(params['size']).toBeUndefined();
  });

  /* Sorting */

  it('should send an ascending sort as the field name', () => {
    const params = new ApiQueryBuilder().sort('createdAt').build();

    expect(params['sort']).toBe('createdAt');
  });

  it('should send a descending sort with a leading dash', () => {
    const params = new ApiQueryBuilder().sort('createdAt', 'desc').build();

    expect(params['sort']).toBe('-createdAt');
  });

  it('should keep one sort clause, the last one given', () => {
    const params = new ApiQueryBuilder().sort('lastName').sort('firstName', 'desc').build();

    expect(params['sort']).toBe('-firstName');
  });

  it('clearSort() should remove the sort clause', () => {
    const params = new ApiQueryBuilder().sort('name').clearSort().build();

    expect(params['sort']).toBeUndefined();
  });

  /* Endpoint parameters */

  it('should add the parameters of the endpoint being called', () => {
    const params = new ApiQueryBuilder().param('status', 'active').param('tenantId', 'tenant-a').build();

    expect(params).toEqual({ status: 'active', tenantId: 'tenant-a' });
  });

  it('extras should override built-in keys when collisions occur', () => {
    const params = new ApiQueryBuilder().setPage(1).param('page', 99).build();

    expect(params['page']).toBe(99);
  });

  /* Full integration */

  it('should compose all parameters together', () => {
    const params = new ApiQueryBuilder().setPage(1).setSize(20).sort('createdAt', 'desc').param('status', 'active').build();

    expect(params).toEqual({ page: 1, size: 20, sort: '-createdAt', status: 'active' });
  });

  it('should emit nothing but the paging parameters the contract declares', () => {
    const params = new ApiQueryBuilder().setPage(1).setSize(10).build();

    expect(Object.keys(params).sort()).toEqual(['page', 'size']);
  });
});
