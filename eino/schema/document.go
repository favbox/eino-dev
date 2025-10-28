package schema

const (
	// 文档子索引键名，用于存储文档的子索引信息，支持分片或分区管理
	docMetaDataKeySubIndexes = "_sub_indexes"

	// 文档评分键名，用于存储文档的相关性评分或质量分数
	docMetaDataKeyScore = "_score"

	// 文档额外信息键名，用于存储自定义的补充信息或扩展数据
	docMetaDataKeyExtraInfo = "_extra_info"

	// 文档DSL键名，用于存储领域特定语言的查询表达式或过滤条件
	docMetaDataKeyDSL = "_dsl"

	// 文档密集向量键名，用于存储高维密集向量数据，适用于语义相似度计算
	docMetaDataKeyDenseVector = "_dense_vector"

	// 文档稀疏向量键名，用于存储稀疏向量数据，适用于关键词匹配或特征表示
	docMetaDataKeySparseVector = "_sparse_vector"
)

// Document - 文档数据结构，包含文本内容和元数据，支持文档检索和元数据管理。
type Document struct {
	// ID - 文档的唯一标识符，用于文档的唯一识别和引用
	ID string `json:"id"`

	// Content - 文档的文本内容，包含主要的文本信息或文档主体
	Content string `json:"content"`

	// MetaData - 文档元数据映射，用于存储评分、向量、索引等补充信息
	MetaData map[string]any `json:"meta_data"`
}

// String - 返回文档的文本内容，实现 Stringer 接口便于输出和调试。
func (d *Document) String() string {
	return d.Content
}

// WithSubIndexes - 设置文档的子索引列表，支持文档分片和分区管理。
// 使用场景：搜索引擎的子索引搜索、文档分片存储、分区查询
func (d *Document) WithSubIndexes(indexes []string) *Document {
	if d.MetaData == nil {
		d.MetaData = make(map[string]any)
	}

	d.MetaData[docMetaDataKeySubIndexes] = indexes

	return d
}

// SubIndexes - 获取文档的子索引列表，返回分片或分区信息。
// 使用场景：子索引搜索、分区查询、文档分布分析
func (d *Document) SubIndexes() []string {
	if d.MetaData == nil {
		return nil
	}

	indexes, ok := d.MetaData[docMetaDataKeySubIndexes].([]string)
	if ok {
		return indexes
	}

	return nil
}

// WithScore - 设置文档的相关性评分或质量分数，用于搜索结果排序。
// 使用场景：搜索引擎排序、推荐系统评分、文档质量评估
func (d *Document) WithScore(score float64) *Document {
	if d.MetaData == nil {
		d.MetaData = make(map[string]any)
	}

	d.MetaData[docMetaDataKeyScore] = score

	return d
}

// Score - 获取文档的相关性评分，返回搜索排序或质量评估分数。
// 使用场景：搜索结果排序、评分计算、性能分析
func (d *Document) Score() float64 {
	if d.MetaData == nil {
		return 0
	}

	score, ok := d.MetaData[docMetaDataKeyScore].(float64)
	if ok {
		return score
	}

	return 0
}

// WithExtraInfo - 设置文档的额外信息字符串，用于存储补充说明或自定义数据。
// 使用场景：文档摘要、用户备注、分类标签、版本信息
func (d *Document) WithExtraInfo(extraInfo string) *Document {
	if d.MetaData == nil {
		d.MetaData = make(map[string]any)
	}

	d.MetaData[docMetaDataKeyExtraInfo] = extraInfo

	return d
}

// ExtraInfo - 获取文档的额外信息字符串，返回补充说明或自定义数据。
// 使用场景：文档摘要显示、用户备注读取、标签信息提取
func (d *Document) ExtraInfo() string {
	if d.MetaData == nil {
		return ""
	}

	extraInfo, ok := d.MetaData[docMetaDataKeyExtraInfo].(string)
	if ok {
		return extraInfo
	}

	return ""
}

// WithDSLInfo - 设置文档的DSL查询信息映射，用于存储结构化查询表达式。
// 使用场景：复杂查询条件存储、过滤条件管理、查询模板构建
func (d *Document) WithDSLInfo(dslInfo map[string]any) *Document {
	if d.MetaData == nil {
		d.MetaData = make(map[string]any)
	}

	d.MetaData[docMetaDataKeyDSL] = dslInfo

	return d
}

// DSLInfo - 获取文档的DSL查询信息映射，返回结构化查询表达式。
// 使用场景：查询条件提取、过滤条件读取、DSL模板解析
func (d *Document) DSLInfo() map[string]any {
	if d.MetaData == nil {
		return nil
	}

	dslInfo, ok := d.MetaData[docMetaDataKeyDSL].(map[string]any)
	if ok {
		return dslInfo
	}

	return nil
}

// WithDenseVector - 设置文档的密集向量数据，用于语义相似度计算和向量搜索。
// 使用场景：神经网络嵌入、语义搜索、推荐系统相似度计算
func (d *Document) WithDenseVector(vector []float64) *Document {
	if d.MetaData == nil {
		d.MetaData = make(map[string]any)
	}

	d.MetaData[docMetaDataKeyDenseVector] = vector

	return d
}

// DenseVector - 获取文档的密集向量数据，返回语义嵌入或特征向量。
// 使用场景：语义搜索、相似度计算、机器学习特征提取
func (d *Document) DenseVector() []float64 {
	if d.MetaData == nil {
		return nil
	}

	vector, ok := d.MetaData[docMetaDataKeyDenseVector].([]float64)
	if ok {
		return vector
	}

	return nil
}

// WithSparseVector - 设置文档的稀疏向量数据，格式为索引-值映射的向量表示。
// 使用场景：TF-IDF向量、关键词匹配、特征工程、词袋模型
func (d *Document) WithSparseVector(sparse map[int]float64) *Document {
	if d.MetaData == nil {
		d.MetaData = make(map[string]any)
	}

	d.MetaData[docMetaDataKeySparseVector] = sparse

	return d
}

// SparseVector - 获取文档的稀疏向量数据，返回索引-值映射的向量表示。
// 使用场景：关键词搜索、TF-IDF计算、稀疏特征处理、推荐系统
func (d *Document) SparseVector() map[int]float64 {
	if d.MetaData == nil {
		return nil
	}

	sparse, ok := d.MetaData[docMetaDataKeySparseVector].(map[int]float64)
	if ok {
		return sparse
	}

	return nil
}
