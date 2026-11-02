import { Empty } from '@cloudflare/kumo';

function FillEmpty({ title, description }) {
  return (
    <div className="flex items-center justify-center p-8">
      <Empty title={title} description={description} />
    </div>
  );
}

export default FillEmpty;